#include "env.h"

#include <stdio.h>
#include <string.h>

static void trim(char *s) {
    size_t len = strlen(s);
    while (len > 0 && (s[len - 1] == '\n' || s[len - 1] == '\r' || s[len - 1] == ' ' || s[len - 1] == '\t')) {
        s[--len] = '\0';
    }
    size_t start = 0;
    while (s[start] == ' ' || s[start] == '\t') start++;
    if (start > 0) memmove(s, s + start, len - start + 1);
}

static void set_field(Config *cfg, const char *key, const char *value) {
    if (strcmp(key, "API_BASE") == 0) {
        snprintf(cfg->api_base, sizeof(cfg->api_base), "%s", value);
    } else if (strcmp(key, "API_CLIENT_ID") == 0) {
        snprintf(cfg->client_id, sizeof(cfg->client_id), "%s", value);
    } else if (strcmp(key, "API_CLIENT_SECRET") == 0) {
        snprintf(cfg->client_secret, sizeof(cfg->client_secret), "%s", value);
    }
}

int env_load(const char *path, Config *cfg) {
    memset(cfg, 0, sizeof(*cfg));

    FILE *f = fopen(path, "r");
    if (!f) return -1;

    char line[512];
    while (fgets(line, sizeof(line), f)) {
        char *nl = strchr(line, '\n');
        if (nl) *nl = '\0';

        char *trimmed = line;
        while (*trimmed == ' ' || *trimmed == '\t') trimmed++;
        if (*trimmed == '\0' || *trimmed == '#') continue;

        char *eq = strchr(trimmed, '=');
        if (!eq) continue;
        *eq = '\0';
        char *key = trimmed;
        char *value = eq + 1;
        trim(key);
        trim(value);

        /* Strip surrounding quotes, if any — a common .env convention. */
        size_t vlen = strlen(value);
        if (vlen >= 2 && ((value[0] == '"' && value[vlen - 1] == '"') || (value[0] == '\'' && value[vlen - 1] == '\''))) {
            value[vlen - 1] = '\0';
            value++;
        }

        set_field(cfg, key, value);
    }
    fclose(f);

    if (cfg->api_base[0] == '\0' || cfg->client_id[0] == '\0' || cfg->client_secret[0] == '\0') {
        return -2;
    }
    return 0;
}
