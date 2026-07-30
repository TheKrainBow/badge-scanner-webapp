#include "local_store.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "vendor/cJSON.h"

static int store_path(char *out, size_t out_len) {
    const char *home = getenv("HOME");
    if (!home || !home[0]) return -1;
    snprintf(out, out_len, "%s/.badge-lookup-gui.json", home);
    return 0;
}

static char *read_whole_file(const char *path, size_t *out_len) {
    FILE *f = fopen(path, "rb");
    if (!f) return NULL;
    if (fseek(f, 0, SEEK_END) != 0) {
        fclose(f);
        return NULL;
    }
    long size = ftell(f);
    if (size < 0 || fseek(f, 0, SEEK_SET) != 0) {
        fclose(f);
        return NULL;
    }
    char *buf = malloc((size_t)size + 1);
    if (!buf) {
        fclose(f);
        return NULL;
    }
    size_t read = fread(buf, 1, (size_t)size, f);
    fclose(f);
    buf[read] = '\0';
    if (out_len) *out_len = read;
    return buf;
}

int local_store_load(LocalMeta **out, int *count) {
    *out = NULL;
    *count = 0;

    char path[512];
    if (store_path(path, sizeof(path)) != 0) return -1;

    size_t len = 0;
    char *data = read_whole_file(path, &len);
    if (!data) return 0; /* missing file is not an error */

    cJSON *json = cJSON_ParseWithLength(data, len);
    free(data);
    if (!json || !cJSON_IsArray(json)) {
        if (json) cJSON_Delete(json);
        return -1;
    }

    int n = cJSON_GetArraySize(json);
    LocalMeta *entries = n > 0 ? calloc((size_t)n, sizeof(LocalMeta)) : NULL;
    if (n > 0 && !entries) {
        cJSON_Delete(json);
        return -1;
    }

    int i = 0;
    const cJSON *item;
    cJSON_ArrayForEach(item, json) {
        if (i >= n) break;
        const cJSON *id = cJSON_GetObjectItemCaseSensitive(item, "id");
        entries[i].id = cJSON_IsNumber(id) ? (long long)id->valuedouble : 0;

        const cJSON *note = cJSON_GetObjectItemCaseSensitive(item, "note");
        if (cJSON_IsString(note) && note->valuestring) {
            snprintf(entries[i].note, sizeof(entries[i].note), "%s", note->valuestring);
        }

        const cJSON *toHandle = cJSON_GetObjectItemCaseSensitive(item, "toHandle");
        entries[i].to_handle = cJSON_IsTrue(toHandle);

        const cJSON *hidden = cJSON_GetObjectItemCaseSensitive(item, "hidden");
        entries[i].hidden = cJSON_IsTrue(hidden);

        i++;
    }
    cJSON_Delete(json);

    *out = entries;
    *count = i;
    return 0;
}

int local_store_save(const LocalMeta *entries, int count) {
    char path[512];
    if (store_path(path, sizeof(path)) != 0) return -1;

    cJSON *arr = cJSON_CreateArray();
    if (!arr) return -1;
    for (int i = 0; i < count; i++) {
        cJSON *item = cJSON_CreateObject();
        cJSON_AddNumberToObject(item, "id", (double)entries[i].id);
        cJSON_AddStringToObject(item, "note", entries[i].note);
        cJSON_AddBoolToObject(item, "toHandle", entries[i].to_handle);
        cJSON_AddBoolToObject(item, "hidden", entries[i].hidden);
        cJSON_AddItemToArray(arr, item);
    }

    char *text = cJSON_PrintUnformatted(arr);
    cJSON_Delete(arr);
    if (!text) return -1;

    /* Write to a temp file then rename — a crash mid-write can't leave a
     * truncated/corrupt state file behind. */
    char tmp_path[520];
    snprintf(tmp_path, sizeof(tmp_path), "%s.tmp", path);
    FILE *f = fopen(tmp_path, "wb");
    if (!f) {
        free(text);
        return -1;
    }
    size_t len = strlen(text);
    size_t written = fwrite(text, 1, len, f);
    fclose(f);
    free(text);
    if (written != len) return -1;

    return rename(tmp_path, path);
}

void local_store_free(LocalMeta *entries) {
    free(entries);
}
