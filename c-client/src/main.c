#include <stdio.h>

#include "display.h"
#include "env.h"
#include "lookup_client.h"
#include "pcsc.h"

static Config g_cfg;
static LookupClient *g_lc;

static void on_tap(const char *uid_hex) {
    LookupResult result;
    if (lookup_client_tap(g_lc, uid_hex, &result) != 0) {
        fprintf(stderr, "lookup failed for %s\n", uid_hex);
        return;
    }
    display_result(uid_hex, &result);
}

int main(int argc, char **argv) {
    const char *env_path = argc > 1 ? argv[1] : ".env";

    int rc = env_load(env_path, &g_cfg);
    if (rc == -1) {
        fprintf(stderr, "couldn't open %s (usage: %s [path/to/.env])\n", env_path, argv[0]);
        return 1;
    }
    if (rc == -2) {
        fprintf(stderr, "%s must set API_BASE, API_CLIENT_ID, and API_CLIENT_SECRET\n", env_path);
        return 1;
    }

    printf("badge-lookup: API_BASE=%s, connecting...\n", g_cfg.api_base);
    g_lc = lookup_client_create(&g_cfg);
    lookup_client_connect(g_lc);
    printf("waiting for a tap...\n");

    return pcsc_run_loop(on_tap) == 0 ? 0 : 1;
}
