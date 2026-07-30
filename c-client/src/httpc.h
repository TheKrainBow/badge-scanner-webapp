#ifndef HTTPC_H
#define HTTPC_H

#include <curl/curl.h>

#include "env.h"

typedef struct {
    long long id; /* the api_key_usage row's id — 0 only on a malformed/legacy response */
    int found;
    char login[128];
    char coalition_name[128];
    char coalition_color[32];
    char coalition_image_url[512];
    char photo_url[512];
} LookupResult;

/* POSTs {"uidHex": uid_hex} to {api_base}/api/lookup with the configured
 * X-Client-Id/X-Client-Secret headers. Returns 0 on a successful HTTP+JSON
 * round-trip (out->found tells you whether the badge actually matched
 * anyone), -1 on a network/HTTP/parse failure. Kept as a fallback/reference
 * implementation — main.c uses the WS path (below) by default. */
int http_lookup(const Config *cfg, const char *uid_hex, LookupResult *out);

/* Downloads photo_url to dest_path. Returns 0 on success. */
int http_download_photo(const char *photo_url, const char *dest_path);

/* Persistent WebSocket connection to {api_base}/api/lookup/ws, using
 * libcurl's native WS support (CURLOPT_CONNECT_ONLY=2, curl_ws_send/recv —
 * available since curl 7.86) rather than a separate WS library, since
 * libcurl is already linked. X-Client-Id/Secret are set as ordinary HTTP
 * headers on the handshake request — something a browser's WebSocket API
 * can't do, which is exactly why this route uses header auth while the
 * dashboard's browser-facing /api/events uses session-cookie auth instead
 * (see backend/internal/api/events_handlers.go's doc comment). */
typedef struct {
    CURL *curl;
} WSConn;

/* Connects and completes the WS handshake. Returns CURLE_OK (0) on success,
 * otherwise the underlying CURLcode — callers should treat
 * CURLE_UNSUPPORTED_PROTOCOL specially: it means this libcurl build has no
 * WebSocket support at all (a compile-time opt-in many distro packages
 * don't enable — WS support existing since curl 7.86 is necessary but not
 * sufficient), so retrying will never succeed and the caller should fall
 * back to the HTTP path (http_lookup) instead. Any other nonzero code is a
 * plain transient failure worth retrying. */
int ws_connect(const Config *cfg, WSConn *conn);

/* Sends {"uidHex": uid_hex} as a single text frame. Returns 0 on success. */
int ws_send_lookup(WSConn *conn, const char *uid_hex);

/* Blocks (with a bounded retry loop, not a busy spin) for the next text
 * frame and parses it as either a LookupResult or a {"error":"..."} frame
 * (logged to stderr, treated as failure). Returns 0 on a successful
 * LookupResult, -1 on a network error, a malformed frame, or an error
 * frame — the caller should treat -1 as "skip this tap", not fatal. */
int ws_recv_result(WSConn *conn, LookupResult *out);

void ws_close(WSConn *conn);

/* One row of this key's own past lookups (see http_fetch_history) — a
 * LookupResult (its .id field is the stable identifier local per-entry
 * state — notes, "to handle", local-only hide, see the GUI's local_store —
 * is keyed on, same one whether learned live or from history) plus the
 * tap's timestamp/uidHex, which a single live lookup doesn't need to carry
 * since the caller already knows those from its own request. */
typedef struct {
    long long timestamp_ms;
    char uid_hex[64];
    LookupResult result;
} HistoryEntry;

/* GETs {api_base}/api/lookup/history?limit=limit with the configured
 * X-Client-Id/Secret headers — returns only the calling key's OWN past
 * lookups (see backend/internal/api/lookup_handlers.go's lookupHistory).
 * Parses the JSON array into out (caller-allocated, capacity max_entries).
 * Returns the number of entries parsed (0..max_entries), or -1 on a
 * network/HTTP/parse failure. */
int http_fetch_history(const Config *cfg, int limit, HistoryEntry *out, int max_entries);

#endif
