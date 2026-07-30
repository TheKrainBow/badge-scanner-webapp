#ifndef LOOKUP_CLIENT_H
#define LOOKUP_CLIENT_H

#include "env.h"
#include "httpc.h"

/* Wraps the WS-connect/HTTP-fallback/reconnect logic shared by the CLI
 * (main.c) and the GUI (gui_main.c), so both get identical connection
 * behavior instead of two copies that could drift. See httpc.h's
 * ws_connect doc comment for what the HTTP fallback is for. */
typedef struct LookupClient LookupClient;

LookupClient *lookup_client_create(const Config *cfg);

/* Blocks until connected (or permanently falls back to HTTP mode) — call
 * once at startup so connection failures/fallback are surfaced before
 * waiting for the first tap, rather than silently on it. lookup_client_tap
 * also calls this internally, so it's not required, just nicer UX. */
void lookup_client_connect(LookupClient *lc);

/* Performs one tap lookup: reuses the existing connection if healthy,
 * transparently (re)connects otherwise. Returns 0 on success. Prints its
 * own connection-status lines to stderr (connecting/reconnecting/falling
 * back to HTTP). */
int lookup_client_tap(LookupClient *lc, const char *uid_hex, LookupResult *out);

void lookup_client_destroy(LookupClient *lc);

#endif
