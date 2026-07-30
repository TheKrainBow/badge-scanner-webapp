#ifndef LOCAL_STORE_H
#define LOCAL_STORE_H

/* GUI-only local metadata, keyed by the server-assigned usage id
 * (HistoryEntry.id, httpc.h) — notes, the "to handle" flag, and
 * locally-hidden entries. Never sent to the backend: "delete" in the GUI
 * only ever sets hidden=1 here and filters that entry out of the rebuilt
 * sidebar on next launch. The server's own history is untouched and
 * unaware this file exists. */
typedef struct {
    long long id;
    char note[256];
    int to_handle;
    int hidden;
} LocalMeta;

/* Loads $HOME/.badge-lookup-gui.json into a newly allocated array (caller
 * frees with local_store_free). A missing file is not an error — *out is
 * set to NULL and *count to 0. Returns 0 on success, -1 on a real error
 * (unreadable/corrupt existing file). */
int local_store_load(LocalMeta **out, int *count);

/* Overwrites $HOME/.badge-lookup-gui.json with entries (rewrites the whole
 * file — simplest correct approach at this scale, no incremental-update
 * complexity needed for a local per-user metadata file). Returns 0 on
 * success. */
int local_store_save(const LocalMeta *entries, int count);

void local_store_free(LocalMeta *entries);

#endif
