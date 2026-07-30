#include "lookup_client.h"

#include <curl/curl.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

typedef enum { MODE_CONNECTING, MODE_WS, MODE_HTTP } Mode;

struct LookupClient {
    Config cfg;
    WSConn ws;
    Mode mode;
};

LookupClient *lookup_client_create(const Config *cfg) {
    LookupClient *lc = calloc(1, sizeof(*lc));
    if (!lc) return NULL;
    lc->cfg = *cfg;
    lc->mode = MODE_CONNECTING;
    return lc;
}

/* Establishes the lookup connection, or falls back to plain HTTP if this
 * libcurl build has no WebSocket support at all (see httpc.h's ws_connect
 * doc comment) — printed once, not retried, since that's a fixed property
 * of the build, not a transient failure. Any other connect failure is
 * retried every 5s, same backoff convention as pcsc_run_loop's reader
 * errors. */
static void ensure_connected(LookupClient *lc) {
    while (lc->mode == MODE_CONNECTING) {
        int rc = ws_connect(&lc->cfg, &lc->ws);
        if (rc == CURLE_OK) {
            lc->mode = MODE_WS;
            printf("connected to %s (websocket)\n", lc->cfg.api_base);
        } else if (rc == CURLE_UNSUPPORTED_PROTOCOL) {
            lc->mode = MODE_HTTP;
            fprintf(stderr,
                "this libcurl build has no WebSocket support (rebuild with --enable-websockets, "
                "or use a distro package that has it) — falling back to one HTTP request per tap\n");
        } else {
            fprintf(stderr, "ws connect failed: %s — retrying in 5s...\n", curl_easy_strerror((CURLcode)rc));
            sleep(5);
        }
    }
}

void lookup_client_connect(LookupClient *lc) {
    ensure_connected(lc);
}

int lookup_client_tap(LookupClient *lc, const char *uid_hex, LookupResult *out) {
    ensure_connected(lc);

    if (lc->mode == MODE_HTTP) {
        return http_lookup(&lc->cfg, uid_hex, out);
    }

    if (ws_send_lookup(&lc->ws, uid_hex) != 0) {
        ws_close(&lc->ws);
        lc->mode = MODE_CONNECTING;
        fprintf(stderr, "lookup failed for %s (connection lost, will reconnect on next tap)\n", uid_hex);
        return -1;
    }

    if (ws_recv_result(&lc->ws, out) != 0) {
        /* Could be a genuine connection break, or just a rate-limit/error
         * frame from an otherwise-healthy connection — not worth
         * distinguishing here, so always reconnect. Slightly wasteful on a
         * plain rate-limit hit, but keeps this simple and self-healing. */
        ws_close(&lc->ws);
        lc->mode = MODE_CONNECTING;
        fprintf(stderr, "lookup failed for %s\n", uid_hex);
        return -1;
    }
    return 0;
}

void lookup_client_destroy(LookupClient *lc) {
    if (!lc) return;
    if (lc->mode == MODE_WS) {
        ws_close(&lc->ws);
    }
    free(lc);
}
