#include "httpc.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include "vendor/cJSON.h"

struct membuf {
    char *data;
    size_t len;
};

static size_t write_to_membuf(void *contents, size_t size, size_t nmemb, void *userp) {
    size_t total = size * nmemb;
    struct membuf *buf = (struct membuf *)userp;
    char *grown = realloc(buf->data, buf->len + total + 1);
    if (!grown) return 0;
    buf->data = grown;
    memcpy(buf->data + buf->len, contents, total);
    buf->len += total;
    buf->data[buf->len] = '\0';
    return total;
}

static void copy_json_string(const cJSON *obj, const char *key, char *dest, size_t dest_len) {
    const cJSON *item = cJSON_GetObjectItemCaseSensitive(obj, key);
    if (cJSON_IsString(item) && item->valuestring) {
        snprintf(dest, dest_len, "%s", item->valuestring);
    } else {
        dest[0] = '\0';
    }
}

/* Shared by the HTTP and WS paths: both receive the exact same LookupResult
 * JSON shape from the backend (performLookup, backend/internal/api/
 * lookup_handlers.go), just over different transports. Also handles a
 * {"error":"..."} frame (sent by the WS path on rate-limit/failure, since
 * there's no HTTP status code to carry that over a raw WS frame) by
 * printing it and returning -1 instead of a false "not found".*/
static int parse_lookup_json(const char *data, size_t len, LookupResult *out) {
    memset(out, 0, sizeof(*out));

    cJSON *json = cJSON_ParseWithLength(data, len);
    if (!json) return -1;

    const cJSON *err = cJSON_GetObjectItemCaseSensitive(json, "error");
    if (cJSON_IsString(err) && err->valuestring) {
        fprintf(stderr, "lookup error: %s\n", err->valuestring);
        cJSON_Delete(json);
        return -1;
    }

    const cJSON *id = cJSON_GetObjectItemCaseSensitive(json, "id");
    out->id = cJSON_IsNumber(id) ? (long long)id->valuedouble : 0;

    const cJSON *found = cJSON_GetObjectItemCaseSensitive(json, "found");
    out->found = cJSON_IsTrue(found);
    if (out->found) {
        copy_json_string(json, "login", out->login, sizeof(out->login));
        copy_json_string(json, "coalitionName", out->coalition_name, sizeof(out->coalition_name));
        copy_json_string(json, "coalitionColor", out->coalition_color, sizeof(out->coalition_color));
        copy_json_string(json, "coalitionImageUrl", out->coalition_image_url, sizeof(out->coalition_image_url));
        copy_json_string(json, "photoUrl", out->photo_url, sizeof(out->photo_url));
    }
    cJSON_Delete(json);
    return 0;
}

int http_lookup(const Config *cfg, const char *uid_hex, LookupResult *out) {
    memset(out, 0, sizeof(*out));

    CURL *curl = curl_easy_init();
    if (!curl) return -1;

    char url[300];
    snprintf(url, sizeof(url), "%s/api/lookup", cfg->api_base);

    char body[128];
    snprintf(body, sizeof(body), "{\"uidHex\":\"%s\"}", uid_hex);

    struct curl_slist *headers = NULL;
    headers = curl_slist_append(headers, "Content-Type: application/json");
    char id_header[160], secret_header[160];
    snprintf(id_header, sizeof(id_header), "X-Client-Id: %s", cfg->client_id);
    snprintf(secret_header, sizeof(secret_header), "X-Client-Secret: %s", cfg->client_secret);
    headers = curl_slist_append(headers, id_header);
    headers = curl_slist_append(headers, secret_header);

    struct membuf resp = {0};

    curl_easy_setopt(curl, CURLOPT_URL, url);
    curl_easy_setopt(curl, CURLOPT_POST, 1L);
    curl_easy_setopt(curl, CURLOPT_POSTFIELDS, body);
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, write_to_membuf);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &resp);
    curl_easy_setopt(curl, CURLOPT_TIMEOUT, 15L);

    CURLcode res = curl_easy_perform(curl);
    long status = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &status);

    curl_slist_free_all(headers);
    curl_easy_cleanup(curl);

    if (res != CURLE_OK) {
        fprintf(stderr, "lookup request failed: %s\n", curl_easy_strerror(res));
        free(resp.data);
        return -1;
    }
    if (status != 200) {
        fprintf(stderr, "lookup request returned HTTP %ld\n", status);
        free(resp.data);
        return -1;
    }

    int rc = parse_lookup_json(resp.data, resp.len, out);
    free(resp.data);
    return rc;
}

int http_download_photo(const char *photo_url, const char *dest_path) {
    CURL *curl = curl_easy_init();
    if (!curl) return -1;

    FILE *f = fopen(dest_path, "wb");
    if (!f) {
        curl_easy_cleanup(curl);
        return -1;
    }

    curl_easy_setopt(curl, CURLOPT_URL, photo_url);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, f);
    curl_easy_setopt(curl, CURLOPT_TIMEOUT, 15L);
    curl_easy_setopt(curl, CURLOPT_FOLLOWLOCATION, 1L);

    CURLcode res = curl_easy_perform(curl);
    long status = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &status);

    fclose(f);
    curl_easy_cleanup(curl);

    if (res != CURLE_OK || status != 200) {
        remove(dest_path);
        return -1;
    }
    return 0;
}

/* {"http(s)://host:port"} -> {"ws(s)://host:port/api/lookup/ws"} — the
 * client only ever configures an http(s) API_BASE (.env doesn't need a
 * separate protocol setting), this derives the WS URL from it. */
static void build_ws_url(const char *api_base, char *out, size_t out_len) {
    const char *rest = strstr(api_base, "://");
    int secure = strncmp(api_base, "https", 5) == 0;
    snprintf(out, out_len, "%s://%s/api/lookup/ws", secure ? "wss" : "ws", rest ? rest + 3 : api_base);
}

int ws_connect(const Config *cfg, WSConn *conn) {
    conn->curl = curl_easy_init();
    if (!conn->curl) return CURLE_FAILED_INIT;

    char url[300];
    build_ws_url(cfg->api_base, url, sizeof(url));

    struct curl_slist *headers = NULL;
    char id_header[160], secret_header[160];
    snprintf(id_header, sizeof(id_header), "X-Client-Id: %s", cfg->client_id);
    snprintf(secret_header, sizeof(secret_header), "X-Client-Secret: %s", cfg->client_secret);
    headers = curl_slist_append(headers, id_header);
    headers = curl_slist_append(headers, secret_header);

    curl_easy_setopt(conn->curl, CURLOPT_URL, url);
    curl_easy_setopt(conn->curl, CURLOPT_HTTPHEADER, headers);
    curl_easy_setopt(conn->curl, CURLOPT_CONNECT_ONLY, 2L); /* 2 = WebSocket mode */

    CURLcode res = curl_easy_perform(conn->curl);
    curl_slist_free_all(headers); /* curl copies these during perform, safe to free now */

    if (res != CURLE_OK) {
        curl_easy_cleanup(conn->curl);
        conn->curl = NULL;
        return (int)res;
    }
    return CURLE_OK;
}

int ws_send_lookup(WSConn *conn, const char *uid_hex) {
    char body[128];
    int n = snprintf(body, sizeof(body), "{\"uidHex\":\"%s\"}", uid_hex);

    size_t sent = 0;
    CURLcode res = curl_ws_send(conn->curl, body, (size_t)n, &sent, 0, CURLWS_TEXT);
    if (res != CURLE_OK) {
        fprintf(stderr, "ws send failed: %s\n", curl_easy_strerror(res));
        return -1;
    }
    return 0;
}

int ws_recv_result(WSConn *conn, LookupResult *out) {
    char buf[4096];
    size_t recvd = 0;
    struct curl_ws_frame *meta = NULL;
    /* curl_ws_recv's metap parameter gained a const qualifier in newer
     * curl (e.g. Homebrew's 8.x) but not in older ones (Debian bookworm's
     * system 7.88.1) — go through void* so this compiles cleanly against
     * either signature without a pointer-constness warning. */
    void *metap = &meta;

    /* curl_ws_recv can return CURLE_AGAIN if no frame has arrived yet on
     * this non-blocking-under-the-hood socket; retry with a short sleep
     * rather than busy-spinning, bounded so a server that never replies
     * doesn't hang this forever. */
    for (int waited_ms = 0; waited_ms < 15000; waited_ms += 20) {
        CURLcode res = curl_ws_recv(conn->curl, buf, sizeof(buf), &recvd, metap);
        if (res == CURLE_OK) {
            return parse_lookup_json(buf, recvd, out);
        }
        if (res != CURLE_AGAIN) {
            fprintf(stderr, "ws recv failed: %s\n", curl_easy_strerror(res));
            return -1;
        }
        usleep(20 * 1000);
    }
    fprintf(stderr, "ws recv timed out waiting for a reply\n");
    return -1;
}

int http_fetch_history(const Config *cfg, int limit, HistoryEntry *out, int max_entries) {
    CURL *curl = curl_easy_init();
    if (!curl) return -1;

    char url[320];
    snprintf(url, sizeof(url), "%s/api/lookup/history?limit=%d", cfg->api_base, limit);

    struct curl_slist *headers = NULL;
    char id_header[160], secret_header[160];
    snprintf(id_header, sizeof(id_header), "X-Client-Id: %s", cfg->client_id);
    snprintf(secret_header, sizeof(secret_header), "X-Client-Secret: %s", cfg->client_secret);
    headers = curl_slist_append(headers, id_header);
    headers = curl_slist_append(headers, secret_header);

    struct membuf resp = {0};

    curl_easy_setopt(curl, CURLOPT_URL, url);
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, write_to_membuf);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &resp);
    curl_easy_setopt(curl, CURLOPT_TIMEOUT, 15L);

    CURLcode res = curl_easy_perform(curl);
    long status = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &status);
    curl_slist_free_all(headers);
    curl_easy_cleanup(curl);

    if (res != CURLE_OK) {
        fprintf(stderr, "fetch history failed: %s\n", curl_easy_strerror(res));
        free(resp.data);
        return -1;
    }
    if (status != 200) {
        fprintf(stderr, "fetch history returned HTTP %ld\n", status);
        free(resp.data);
        return -1;
    }

    cJSON *json = cJSON_ParseWithLength(resp.data, resp.len);
    free(resp.data);
    if (!json || !cJSON_IsArray(json)) {
        if (json) cJSON_Delete(json);
        return -1;
    }

    int n = 0;
    const cJSON *item;
    cJSON_ArrayForEach(item, json) {
        if (n >= max_entries) break;
        HistoryEntry *e = &out[n];
        memset(e, 0, sizeof(*e));

        const cJSON *id = cJSON_GetObjectItemCaseSensitive(item, "id");
        e->result.id = cJSON_IsNumber(id) ? (long long)id->valuedouble : 0;
        const cJSON *ts = cJSON_GetObjectItemCaseSensitive(item, "timestamp");
        e->timestamp_ms = cJSON_IsNumber(ts) ? (long long)ts->valuedouble : 0;
        copy_json_string(item, "uidHex", e->uid_hex, sizeof(e->uid_hex));

        const cJSON *found = cJSON_GetObjectItemCaseSensitive(item, "found");
        e->result.found = cJSON_IsTrue(found);
        copy_json_string(item, "login", e->result.login, sizeof(e->result.login));
        copy_json_string(item, "coalitionName", e->result.coalition_name, sizeof(e->result.coalition_name));
        copy_json_string(item, "coalitionColor", e->result.coalition_color, sizeof(e->result.coalition_color));
        copy_json_string(item, "coalitionImageUrl", e->result.coalition_image_url, sizeof(e->result.coalition_image_url));
        copy_json_string(item, "photoUrl", e->result.photo_url, sizeof(e->result.photo_url));

        n++;
    }
    cJSON_Delete(json);
    return n;
}

void ws_close(WSConn *conn) {
    if (conn->curl) {
        curl_easy_cleanup(conn->curl);
        conn->curl = NULL;
    }
}
