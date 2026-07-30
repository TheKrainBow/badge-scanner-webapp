#ifndef ENV_H
#define ENV_H

typedef struct {
    char api_base[256];
    char client_id[128];
    char client_secret[128];
} Config;

/* Loads KEY=VALUE lines from path into cfg. Returns 0 on success, -1 if the
 * file can't be opened, -2 if a required key is missing. */
int env_load(const char *path, Config *cfg);

#endif
