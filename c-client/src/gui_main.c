/* GTK3 desktop GUI: a sidebar of recent badge taps (newest on top,
 * rebuilt from the server's own history on startup — see
 * httpc.h's http_fetch_history) plus a detail panel (photo, login,
 * coalition, a free-text note, a "to handle" flag shown as a red pin in
 * the sidebar, and a delete button that only ever hides the entry
 * locally — see local_store.h's doc comment for why the server never
 * hears about that).
 *
 * GTK owns the main thread for its event loop; pcsc_run_loop blocks
 * forever polling the reader, so it runs on its own pthread, and every UI
 * update it triggers is marshaled onto the main thread via g_idle_add —
 * GTK widgets aren't safe to touch from any other thread. */
#include <gtk/gtk.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <time.h>

#include "display.h"
#include "env.h"
#include "httpc.h"
#include "local_store.h"
#include "lookup_client.h"
#include "pcsc.h"

#define HISTORY_LIMIT 200

typedef struct {
    long long id;
    char uid_hex[64];
    long long timestamp_ms;
    int found;
    char login[128];
    char coalition_name[128];
    char coalition_color[32];
    char coalition_image_url[512];
    char photo_url[512];
    char note[256];
    int to_handle;
    int hidden;
    char photo_path[300];           /* cached user-photo download path, empty until loaded */
    char coalition_image_path[300]; /* cached coalition-logo download path, empty until loaded */
    GtkWidget *row;                 /* GtkListBoxRow */
    GtkWidget *pin_label;
} GuiEntry;

/* Same red used for "danger"/destructive actions across the whole project
 * (webapp/frontend/src/index.css's --danger) — reused here for both the
 * "to handle" pin and the session-start separator, for visual consistency
 * with the rest of the app. */
#define ACCENT_RED "#d3465a"

static Config g_cfg;
static LookupClient *g_lc;
static GPtrArray *g_entries; /* GuiEntry* */

static GtkWidget *g_listbox;
static GtkWidget *g_detail; /* right-hand panel; background tinted per-selection, see update_detail_background */
static GtkWidget *g_coalition_bg_image; /* big, faded, tinted coalition logo behind the whole panel */
static GtkWidget *g_photo_image;
static GtkWidget *g_login_link; /* GtkLinkButton: clicking it opens the 42 intra profile */
static GtkWidget *g_coalition_image;
static GtkWidget *g_coalition_label;
static GtkWidget *g_handle_btn;
static GtkTextBuffer *g_note_buffer;
static GuiEntry *g_selected;
static GtkCssProvider *g_detail_css; /* reloaded with a new background-color on every selection */

/* Turns "#RRGGBB" into a translucent rgba() CSS value and applies it as
 * #detail-panel's background — a solid fill would fight with the photo/text
 * on top of it and with light coalition colors on the dark theme, so this
 * tints rather than floods. Falls back to no tint (theme default) when the
 * entry has no coalition color at all. */
static void update_detail_background(const char *hex_color) {
    char css[512];
    unsigned int r, g, b;
    if (hex_color && hex_color[0] == '#' && sscanf(hex_color, "#%2x%2x%2x", &r, &g, &b) == 3) {
        /* Two solid (not alpha-blended — a translucent overlay tint read as
         * barely-there against the dark theme's own backdrop) shades of the
         * same coalition color: the panel gets a moderately-bright one, the
         * note field + buttons a visibly darker one, so the two stay
         * distinguishable while both read as "that coalition's color".
         * background-image: none overrides the theme's default button
         * gradient, which would otherwise hide a flat background-color. */
        unsigned int pr = r * 11 / 20, pg = g * 11 / 20, pb = b * 11 / 20;
        unsigned int dr = r * 3 / 10, dg = g * 3 / 10, db = b * 3 / 10;
        snprintf(css, sizeof(css),
            /* border-color matches background-color everywhere here so the
             * theme's default gray outline never shows as a separate line
             * around the panel, the note field, or the buttons. */
            "#detail-panel { background-color: rgb(%u, %u, %u); border-color: rgb(%u, %u, %u); }"
            /* "button.detail-btn" (element+class) outranks the theme's own
             * "button" element rules — the bare ".detail-btn" class
             * selector used before this didn't have enough specificity to
             * win, so the buttons stayed the theme's default gray. */
            "#detail-note, #detail-note text, button.detail-btn { background-color: rgb(%u, %u, %u); background-image: none; border-color: rgb(%u, %u, %u); }",
            pr, pg, pb, pr, pg, pb, dr, dg, db, dr, dg, db);
    } else {
        snprintf(css, sizeof(css),
            "#detail-panel { background-color: transparent; border-color: initial; }"
            "#detail-note, #detail-note text, button.detail-btn { background-color: transparent; background-image: initial; border-color: initial; }");
    }
    gtk_css_provider_load_from_data(g_detail_css, css, -1, NULL);
}

static void update_pin_label(GuiEntry *e) {
    if (e->to_handle) {
        gtk_label_set_markup(GTK_LABEL(e->pin_label), "<span foreground='" ACCENT_RED "'>●</span>");
    } else {
        gtk_label_set_text(GTK_LABEL(e->pin_label), " ");
    }
}

static GtkWidget *build_row_widget(GuiEntry *e) {
    GtkWidget *row = gtk_list_box_row_new();
    GtkWidget *hbox = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 6);
    gtk_container_set_border_width(GTK_CONTAINER(hbox), 4);

    GtkWidget *pin = gtk_label_new("");
    gtk_label_set_width_chars(GTK_LABEL(pin), 1);
    e->pin_label = pin;
    update_pin_label(e);

    GtkWidget *vbox = gtk_box_new(GTK_ORIENTATION_VERTICAL, 0);

    /* "login (Coalition)" with the coalition part bold and in its own color;
     * %s substitutions are auto-escaped by g_markup_printf_escaped, so the
     * raw login/name/color are safe to pass through. */
    GtkWidget *name_label = gtk_label_new(NULL);
    const char *name_text = e->found ? e->login : "unknown";
    if (e->coalition_name[0] && e->coalition_color[0]) {
        char *markup = g_markup_printf_escaped(
            "%s (<span foreground='%s'><b>%s</b></span>)",
            name_text, e->coalition_color, e->coalition_name);
        gtk_label_set_markup(GTK_LABEL(name_label), markup);
        g_free(markup);
    } else if (e->coalition_name[0]) {
        char *markup = g_markup_printf_escaped("%s (<b>%s</b>)", name_text, e->coalition_name);
        gtk_label_set_markup(GTK_LABEL(name_label), markup);
        g_free(markup);
    } else {
        gtk_label_set_text(GTK_LABEL(name_label), name_text);
    }
    gtk_widget_set_halign(name_label, GTK_ALIGN_START);
    gtk_label_set_ellipsize(GTK_LABEL(name_label), PANGO_ELLIPSIZE_END);
    gtk_box_pack_start(GTK_BOX(vbox), name_label, FALSE, FALSE, 0);

    char time_buf[32];
    time_t tap_time = (time_t)(e->timestamp_ms / 1000);
    struct tm tm_tap;
    localtime_r(&tap_time, &tm_tap);
    strftime(time_buf, sizeof(time_buf), "%d/%m/%Y %H:%M:%S", &tm_tap);
    GtkWidget *time_label = gtk_label_new(time_buf);
    gtk_widget_set_halign(time_label, GTK_ALIGN_START);
    /* Row width is fixed by the sidebar's own size request (below); ellipsize
     * as a safety net so a wide theme font can never force a horizontal
     * scrollbar we've deliberately disabled. */
    gtk_label_set_ellipsize(GTK_LABEL(time_label), PANGO_ELLIPSIZE_END);
    /* "dim-label" is a built-in GTK3 style class most themes render
     * muted/smaller — no custom CSS needed for this one. */
    gtk_style_context_add_class(gtk_widget_get_style_context(time_label), "dim-label");
    gtk_box_pack_start(GTK_BOX(vbox), time_label, FALSE, FALSE, 0);

    gtk_box_pack_start(GTK_BOX(hbox), pin, FALSE, FALSE, 0);
    gtk_box_pack_start(GTK_BOX(hbox), vbox, TRUE, TRUE, 0);
    gtk_container_add(GTK_CONTAINER(row), hbox);

    g_object_set_data(G_OBJECT(row), "entry", e);
    e->row = row;
    return row;
}

/* Rewrites the whole local state file from the current in-memory entries —
 * simplest correct approach at this scale (a few hundred entries at most),
 * no need for incremental updates. */
static void save_local_store_now(void) {
    LocalMeta *metas = calloc(g_entries->len, sizeof(LocalMeta));
    if (!metas) return;
    for (guint i = 0; i < g_entries->len; i++) {
        GuiEntry *e = g_ptr_array_index(g_entries, i);
        metas[i].id = e->id;
        snprintf(metas[i].note, sizeof(metas[i].note), "%s", e->note);
        metas[i].to_handle = e->to_handle;
        metas[i].hidden = e->hidden;
    }
    local_store_save(metas, (int)g_entries->len);
    free(metas);
}

static void ensure_photo_loaded(GuiEntry *e) {
    if (e->photo_path[0] || !e->photo_url[0]) return;
    char dest[300];
    snprintf(dest, sizeof(dest), "/tmp/badge-lookup-gui-%s.%s", e->uid_hex, photo_extension(e->photo_url));
    if (http_download_photo(e->photo_url, dest) == 0) {
        snprintf(e->photo_path, sizeof(e->photo_path), "%s", dest);
    }
}

/* Same idea as ensure_photo_loaded, separate cache path since a badge's
 * user photo and its coalition's logo are different images. Downloading
 * per-entry (rather than de-duplicating by coalition) is mildly wasteful
 * when several badges share a coalition, but keeps this simple and the
 * dataset here is small enough (a handful of taps) that it doesn't matter. */
static void ensure_coalition_image_loaded(GuiEntry *e) {
    if (e->coalition_image_path[0] || !e->coalition_image_url[0]) return;
    char dest[300];
    snprintf(dest, sizeof(dest), "/tmp/badge-lookup-gui-coalition-%s.%s", e->uid_hex, photo_extension(e->coalition_image_url));
    if (http_download_photo(e->coalition_image_url, dest) == 0) {
        snprintf(e->coalition_image_path, sizeof(e->coalition_image_path), "%s", dest);
    }
}

/* Loads a downloaded image file into a GtkImage at the given size, or
 * clears it if path is empty/unloadable. Shared by the coalition-logo
 * display in the detail panel and (as a base before tinting) the big
 * background decoration. */
static void set_image_from_path(GtkWidget *image, const char *path, int size) {
    if (!path[0]) {
        gtk_image_clear(GTK_IMAGE(image));
        return;
    }
    GError *err = NULL;
    GdkPixbuf *pix = gdk_pixbuf_new_from_file_at_scale(path, size, size, TRUE, &err);
    if (pix) {
        gtk_image_set_from_pixbuf(GTK_IMAGE(image), pix);
        g_object_unref(pix);
    } else {
        gtk_image_clear(GTK_IMAGE(image));
        if (err) g_error_free(err);
    }
}

/* Loads path as a size x size square (cropping to fill, not letterboxing —
 * fine for headshots) and clips it to a circle, for the profile photo. */
static void set_circular_photo(GtkWidget *image, const char *path, int size) {
    if (!path[0]) {
        gtk_image_clear(GTK_IMAGE(image));
        return;
    }
    GError *err = NULL;
    GdkPixbuf *square = gdk_pixbuf_new_from_file_at_scale(path, size, size, FALSE, &err);
    if (!square) {
        gtk_image_clear(GTK_IMAGE(image));
        if (err) g_error_free(err);
        return;
    }
    cairo_surface_t *surf = cairo_image_surface_create(CAIRO_FORMAT_ARGB32, size, size);
    cairo_t *cr = cairo_create(surf);
    cairo_arc(cr, size / 2.0, size / 2.0, size / 2.0, 0, 2 * G_PI);
    cairo_clip(cr);
    gdk_cairo_set_source_pixbuf(cr, square, 0, 0);
    cairo_paint(cr);
    cairo_destroy(cr);
    g_object_unref(square);
    GdkPixbuf *circular = gdk_pixbuf_get_from_surface(surf, 0, 0, size, size);
    cairo_surface_destroy(surf);
    if (circular) {
        gtk_image_set_from_pixbuf(GTK_IMAGE(image), circular);
        g_object_unref(circular);
    } else {
        gtk_image_clear(GTK_IMAGE(image));
    }
}

/* Loads path at a large decorative size and tints its opaque pixels toward
 * (r,g,b) via CAIRO_OPERATOR_ATOP — blends a multi-color logo into a single
 * coalition-color-ish image without needing per-pixel color analysis.
 * Overall fade to "stay as decoration" (not compete with the panel's
 * foreground text) is applied separately, as the widget's opacity — see
 * update_coalition_background. */
static GdkPixbuf *load_tinted_logo(const char *path, int size, unsigned int r, unsigned int g, unsigned int b) {
    GError *err = NULL;
    GdkPixbuf *base = gdk_pixbuf_new_from_file_at_scale(path, size, size, TRUE, &err);
    if (!base) {
        if (err) g_error_free(err);
        return NULL;
    }
    int w = gdk_pixbuf_get_width(base), h = gdk_pixbuf_get_height(base);
    cairo_surface_t *surf = cairo_image_surface_create(CAIRO_FORMAT_ARGB32, w, h);
    cairo_t *cr = cairo_create(surf);
    gdk_cairo_set_source_pixbuf(cr, base, 0, 0);
    cairo_paint(cr);
    cairo_set_operator(cr, CAIRO_OPERATOR_ATOP);
    cairo_set_source_rgba(cr, r / 255.0, g / 255.0, b / 255.0, 0.55);
    cairo_paint(cr);
    cairo_destroy(cr);
    g_object_unref(base);
    GdkPixbuf *tinted = gdk_pixbuf_get_from_surface(surf, 0, 0, w, h);
    cairo_surface_destroy(surf);
    return tinted;
}

/* Big, faded, color-tinted coalition logo behind the whole detail panel —
 * "so it stays as decoration" rather than competing with the photo/text/
 * buttons on top of it (see the GtkOverlay wiring in main). */
static void update_coalition_background(GuiEntry *e) {
    if (!e || !e->coalition_image_path[0]) {
        gtk_image_clear(GTK_IMAGE(g_coalition_bg_image));
        return;
    }
    unsigned int r = 255, g = 255, b = 255;
    if (e->coalition_color[0] == '#') sscanf(e->coalition_color, "#%2x%2x%2x", &r, &g, &b);
    GdkPixbuf *tinted = load_tinted_logo(e->coalition_image_path, 320, r, g, b);
    if (tinted) {
        gtk_image_set_from_pixbuf(GTK_IMAGE(g_coalition_bg_image), tinted);
        g_object_unref(tinted);
    } else {
        gtk_image_clear(GTK_IMAGE(g_coalition_bg_image));
    }
}

static void show_detail(GuiEntry *e) {
    g_selected = e;
    gtk_button_set_label(GTK_BUTTON(g_login_link), e->found ? e->login : "unknown");
    if (e->found && e->login[0]) {
        char uri[256];
        snprintf(uri, sizeof(uri), "https://profile.intra.42.fr/users/%s", e->login);
        gtk_link_button_set_uri(GTK_LINK_BUTTON(g_login_link), uri);
        gtk_widget_set_sensitive(g_login_link, TRUE);
    } else {
        /* No known login to link to (badge tapped but not matched to a 42
         * account) — keep the label but don't let it try to open a bogus URL. */
        gtk_widget_set_sensitive(g_login_link, FALSE);
    }

    gtk_text_buffer_set_text(g_note_buffer, e->note, -1);
    gtk_button_set_label(GTK_BUTTON(g_handle_btn), e->to_handle ? "Mark as handled" : "Mark as to handle");

    if (e->coalition_name[0] && e->coalition_color[0]) {
        /* %s substitutions are auto-escaped by g_markup_printf_escaped, so
         * passing the raw (unescaped) name/color through here is safe. */
        char *markup = g_markup_printf_escaped("<span foreground='%s'><b>%s</b></span>", e->coalition_color, e->coalition_name);
        gtk_label_set_markup(GTK_LABEL(g_coalition_label), markup);
        g_free(markup);
    } else {
        gtk_label_set_text(GTK_LABEL(g_coalition_label), e->coalition_name);
    }
    update_detail_background(e->coalition_color[0] ? e->coalition_color : NULL);

    ensure_photo_loaded(e);
    set_circular_photo(g_photo_image, e->photo_path, 96);

    ensure_coalition_image_loaded(e);
    set_image_from_path(g_coalition_image, e->coalition_image_path, 28);
    update_coalition_background(e);
}

static void on_row_selected(GtkListBox *box, GtkListBoxRow *row, gpointer user_data) {
    (void)box;
    (void)user_data;
    if (!row) {
        g_selected = NULL;
        return;
    }
    GuiEntry *e = g_object_get_data(G_OBJECT(row), "entry");
    if (e) show_detail(e);
}

static void on_save_note_clicked(GtkButton *btn, gpointer user_data) {
    (void)btn;
    (void)user_data;
    if (!g_selected) return;
    GtkTextIter start, end;
    gtk_text_buffer_get_bounds(g_note_buffer, &start, &end);
    char *text = gtk_text_buffer_get_text(g_note_buffer, &start, &end, FALSE);
    snprintf(g_selected->note, sizeof(g_selected->note), "%s", text);
    g_free(text);
    save_local_store_now();
}

static void on_toggle_handle_clicked(GtkButton *btn, gpointer user_data) {
    (void)user_data;
    if (!g_selected) return;
    g_selected->to_handle = !g_selected->to_handle;
    update_pin_label(g_selected);
    gtk_button_set_label(btn, g_selected->to_handle ? "Mark as handled" : "Mark as to handle");
    save_local_store_now();
}

/* Only hides the entry locally (this process's state file) and removes it
 * from this window — never calls any backend delete route (there isn't
 * one, and there shouldn't be: the server is the source of truth for what
 * badges were actually tapped, this is just this operator's own view). */
static void on_delete_clicked(GtkButton *btn, gpointer user_data) {
    (void)btn;
    (void)user_data;
    if (!g_selected) return;

    g_selected->hidden = 1;
    save_local_store_now();

    GtkWidget *row = g_selected->row;
    g_ptr_array_remove(g_entries, g_selected);
    gtk_widget_destroy(row);
    free(g_selected);
    g_selected = NULL;

    gtk_button_set_label(GTK_BUTTON(g_login_link), "");
    gtk_widget_set_sensitive(g_login_link, FALSE);
    gtk_label_set_text(GTK_LABEL(g_coalition_label), "");
    gtk_text_buffer_set_text(g_note_buffer, "", -1);
    gtk_image_clear(GTK_IMAGE(g_photo_image));
    gtk_image_clear(GTK_IMAGE(g_coalition_image));
    gtk_image_clear(GTK_IMAGE(g_coalition_bg_image));
    update_detail_background(NULL);
}

static GuiEntry *entry_new(long long id, const char *uid_hex, long long timestamp_ms, const LookupResult *r) {
    GuiEntry *e = calloc(1, sizeof(*e));
    if (!e) return NULL;
    e->id = id;
    snprintf(e->uid_hex, sizeof(e->uid_hex), "%s", uid_hex);
    e->timestamp_ms = timestamp_ms;
    e->found = r->found;
    snprintf(e->login, sizeof(e->login), "%s", r->login);
    snprintf(e->coalition_name, sizeof(e->coalition_name), "%s", r->coalition_name);
    snprintf(e->coalition_color, sizeof(e->coalition_color), "%s", r->coalition_color);
    snprintf(e->coalition_image_url, sizeof(e->coalition_image_url), "%s", r->coalition_image_url);
    snprintf(e->photo_url, sizeof(e->photo_url), "%s", r->photo_url);
    return e;
}

static void apply_local_meta(GuiEntry *e, const LocalMeta *metas, int meta_count) {
    for (int i = 0; i < meta_count; i++) {
        if (metas[i].id == e->id) {
            snprintf(e->note, sizeof(e->note), "%s", metas[i].note);
            e->to_handle = metas[i].to_handle;
            e->hidden = metas[i].hidden;
            return;
        }
    }
}

/* Runs on the GTK main thread (via g_idle_add from the background PC/SC
 * thread, below) — this is the only place a freshly-tapped badge touches
 * any GTK widget. */
static gboolean idle_new_tap(gpointer data) {
    HistoryEntry *h = (HistoryEntry *)data;
    GuiEntry *e = entry_new(h->result.id, h->uid_hex, h->timestamp_ms, &h->result);
    free(h);
    if (!e) return FALSE;

    GtkWidget *row = build_row_widget(e);
    g_ptr_array_add(g_entries, e);
    gtk_list_box_prepend(GTK_LIST_BOX(g_listbox), row);
    gtk_widget_show_all(row);
    /* "when we badge, it should be selected so we see it on the right" —
     * triggers on_row_selected -> show_detail for the badge that was just
     * tapped, same as clicking it manually. */
    gtk_list_box_select_row(GTK_LIST_BOX(g_listbox), GTK_LIST_BOX_ROW(row));
    return FALSE;
}

/* Runs on the background PC/SC thread — network I/O here is fine (it's not
 * the GTK main thread), but nothing here may touch a GTK widget directly;
 * idle_new_tap (scheduled via g_idle_add, itself thread-safe to call from
 * any thread) does that part on the main thread instead. */
static void on_tap(const char *uid_hex) {
    LookupResult result;
    if (lookup_client_tap(g_lc, uid_hex, &result) != 0) {
        fprintf(stderr, "lookup failed for %s\n", uid_hex);
        return;
    }
    print_tap_line(uid_hex, &result); /* terminal log, same format as the CLI */

    HistoryEntry *transfer = calloc(1, sizeof(*transfer));
    if (!transfer) return;
    transfer->timestamp_ms = (long long)time(NULL) * 1000;
    snprintf(transfer->uid_hex, sizeof(transfer->uid_hex), "%s", uid_hex);
    transfer->result = result;

    g_idle_add(idle_new_tap, transfer);
}

static void *pcsc_thread_func(void *arg) {
    (void)arg;
    pcsc_run_loop(on_tap);
    return NULL;
}

int main(int argc, char **argv) {
    gtk_init(&argc, &argv);

    /* Dark theme: ask the current GTK theme to render its dark variant
     * (supported by Adwaita and most modern themes) rather than hand-
     * rolling custom colors — simpler and stays consistent with whatever
     * theme the user already has. */
    g_object_set(gtk_settings_get_default(), "gtk-application-prefer-dark-theme", TRUE, NULL);

    /* Registered once, applied to the session-start separator inserted
     * after history loads, below. */
    GtkCssProvider *css = gtk_css_provider_new();
    gtk_css_provider_load_from_data(css,
        "separator.session-marker { background-color: " ACCENT_RED "; min-height: 3px; }"
        /* Delete sits next to "Mark as to handle" and would otherwise match
         * its width exactly; trimming its own padding keeps the row from
         * stretching further than it needs to. */
        ".detail-btn-delete { padding-left: 8px; padding-right: 8px; }"
        /* GTK reserves an invisible resize-grab margin around the whole
         * window (the "decoration" CSS node) even for a CSD-less window;
         * on a compositor it's transparent, but without one it can render
         * as solid gray — showing up as a border around the entire app,
         * most visible against the now-colored detail panel. Zeroing it out
         * removes that margin outright. */
        "decoration { margin: 0; border: none; box-shadow: none; }",
        -1, NULL);
    gtk_style_context_add_provider_for_screen(gdk_screen_get_default(), GTK_STYLE_PROVIDER(css), GTK_STYLE_PROVIDER_PRIORITY_APPLICATION);
    g_object_unref(css);

    const char *env_path = ".env";
    if (argc > 1) env_path = argv[1];

    int rc = env_load(env_path, &g_cfg);
    if (rc == -1) {
        fprintf(stderr, "couldn't open %s (usage: %s [path/to/.env])\n", env_path, argv[0]);
        return 1;
    }
    if (rc == -2) {
        fprintf(stderr, "%s must set API_BASE, API_CLIENT_ID, and API_CLIENT_SECRET\n", env_path);
        return 1;
    }

    g_entries = g_ptr_array_new();
    g_lc = lookup_client_create(&g_cfg);

    GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
    gtk_window_set_title(GTK_WINDOW(window), "Badge Lookup");
    gtk_window_set_default_size(GTK_WINDOW(window), 700, 500);
    g_signal_connect(window, "destroy", G_CALLBACK(gtk_main_quit), NULL);

    GtkWidget *hbox = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 0);
    gtk_container_add(GTK_CONTAINER(window), hbox);

    /* Sidebar: GtkScrolledWindow gives native scrolling for free. Wide
     * enough for the "DD/MM/YYYY HH:MM:SS" timestamp row without wrapping. */
    GtkWidget *scroll = gtk_scrolled_window_new(NULL, NULL);
    gtk_scrolled_window_set_policy(GTK_SCROLLED_WINDOW(scroll), GTK_POLICY_NEVER, GTK_POLICY_AUTOMATIC);
    gtk_widget_set_size_request(scroll, 260, -1);
    g_listbox = gtk_list_box_new();
    g_signal_connect(g_listbox, "row-selected", G_CALLBACK(on_row_selected), NULL);
    gtk_container_add(GTK_CONTAINER(scroll), g_listbox);
    gtk_box_pack_start(GTK_BOX(hbox), scroll, FALSE, FALSE, 0);

    /* Detail panel = a GtkOverlay with three layers, bottom to top:
     *   1. main child `detail_bg` — carries ONLY the coalition-color
     *      background. A GtkOverlay always allocates its main child the
     *      overlay's full size, so this is what guarantees the color reaches
     *      every edge. (Every earlier gray-frame bug came from putting the
     *      color on an *overlay* child instead: overlay children are sized/
     *      positioned by their own alignment and don't reliably fill to the
     *      very edge, so a few px of the overlay's own non-painting area —
     *      i.e. the window's gray — showed through as a frame.)
     *   2. overlay `g_coalition_bg_image` — the big faded logo decoration.
     *   3. overlay `detail` — the actual content (photo/login/note/buttons),
     *      transparent so the color + logo below show through.
     * GtkOverlay itself paints no background of its own, which is why the
     * color lives on the main child rather than on the overlay. */
    GtkWidget *detail_overlay = gtk_overlay_new();
    gtk_widget_set_hexpand(detail_overlay, TRUE);
    gtk_widget_set_vexpand(detail_overlay, TRUE);
    gtk_box_pack_start(GTK_BOX(hbox), detail_overlay, TRUE, TRUE, 0);

    GtkWidget *detail_bg = gtk_box_new(GTK_ORIENTATION_VERTICAL, 0);
    gtk_widget_set_name(detail_bg, "detail-panel"); /* update_detail_background's #detail-panel background-color target */
    gtk_container_add(GTK_CONTAINER(detail_overlay), detail_bg);

    g_coalition_bg_image = gtk_image_new();
    gtk_widget_set_halign(g_coalition_bg_image, GTK_ALIGN_CENTER);
    gtk_widget_set_valign(g_coalition_bg_image, GTK_ALIGN_CENTER);
    /* The logo is already color-tinted (load_tinted_logo); fading the whole
     * widget on top of that is what keeps it "as decoration" instead of
     * competing with the photo/text/buttons drawn over it. */
    gtk_widget_set_opacity(g_coalition_bg_image, 0.28);
    gtk_overlay_add_overlay(GTK_OVERLAY(detail_overlay), g_coalition_bg_image);

    GtkWidget *detail = gtk_box_new(GTK_ORIENTATION_VERTICAL, 8);
    g_detail = detail;
    gtk_container_set_border_width(GTK_CONTAINER(detail), 12);
    gtk_widget_set_hexpand(detail, TRUE);
    gtk_widget_set_vexpand(detail, TRUE);
    gtk_overlay_add_overlay(GTK_OVERLAY(detail_overlay), detail);

    /* Applied screen-wide (like the session-marker provider below) rather
     * than just to `detail`'s own style context, since its rules target
     * descendants (#detail-note text, .detail-btn) that have their own
     * separate style contexts a widget-scoped provider wouldn't reach. */
    g_detail_css = gtk_css_provider_new();
    gtk_style_context_add_provider_for_screen(gdk_screen_get_default(), GTK_STYLE_PROVIDER(g_detail_css), GTK_STYLE_PROVIDER_PRIORITY_APPLICATION);
    update_detail_background(NULL);

    /* Header row: a smaller circular photo on the left, login + coalition
     * stacked to its right, instead of the photo stacked above them. */
    GtkWidget *header_row = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 12);
    g_photo_image = gtk_image_new();
    gtk_box_pack_start(GTK_BOX(header_row), g_photo_image, FALSE, FALSE, 0);

    GtkWidget *header_text = gtk_box_new(GTK_ORIENTATION_VERTICAL, 4);
    gtk_widget_set_valign(header_text, GTK_ALIGN_CENTER);

    /* GtkLinkButton renders as a hyperlink and opens its URI (via the
     * desktop's default handler) when clicked — "clicking heinz should open
     * a browser on the profile". "flat" strips the button chrome so it
     * reads as plain clickable text, not a button. */
    g_login_link = gtk_link_button_new_with_label("", "");
    gtk_style_context_add_class(gtk_widget_get_style_context(g_login_link), "flat");
    gtk_widget_set_halign(g_login_link, GTK_ALIGN_START);
    gtk_box_pack_start(GTK_BOX(header_text), g_login_link, FALSE, FALSE, 0);

    GtkWidget *coalition_row = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 6);
    g_coalition_image = gtk_image_new();
    gtk_box_pack_start(GTK_BOX(coalition_row), g_coalition_image, FALSE, FALSE, 0);
    g_coalition_label = gtk_label_new("");
    gtk_widget_set_halign(g_coalition_label, GTK_ALIGN_START);
    gtk_box_pack_start(GTK_BOX(coalition_row), g_coalition_label, FALSE, FALSE, 0);
    gtk_box_pack_start(GTK_BOX(header_text), coalition_row, FALSE, FALSE, 0);

    gtk_box_pack_start(GTK_BOX(header_row), header_text, FALSE, FALSE, 0);
    gtk_box_pack_start(GTK_BOX(detail), header_row, FALSE, FALSE, 0);

    GtkWidget *note_scroll = gtk_scrolled_window_new(NULL, NULL);
    gtk_widget_set_size_request(note_scroll, -1, 100);
    GtkWidget *note_view = gtk_text_view_new();
    gtk_widget_set_name(note_view, "detail-note"); /* targeted by update_detail_background */
    gtk_text_view_set_wrap_mode(GTK_TEXT_VIEW(note_view), GTK_WRAP_WORD);
    g_note_buffer = gtk_text_view_get_buffer(GTK_TEXT_VIEW(note_view));
    gtk_container_add(GTK_CONTAINER(note_scroll), note_view);
    gtk_box_pack_start(GTK_BOX(detail), note_scroll, TRUE, TRUE, 0);

    GtkWidget *save_note_btn = gtk_button_new_with_label("Save note");
    gtk_style_context_add_class(gtk_widget_get_style_context(save_note_btn), "detail-btn");
    g_signal_connect(save_note_btn, "clicked", G_CALLBACK(on_save_note_clicked), NULL);
    gtk_box_pack_start(GTK_BOX(detail), save_note_btn, FALSE, FALSE, 0);

    /* halign START (instead of the vertical box's default FILL) so this row
     * only takes the width its buttons actually need, not the full panel. */
    GtkWidget *btn_row = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 6);
    gtk_widget_set_halign(btn_row, GTK_ALIGN_START);
    g_handle_btn = gtk_button_new_with_label("Mark as to handle");
    gtk_style_context_add_class(gtk_widget_get_style_context(g_handle_btn), "detail-btn");
    g_signal_connect(g_handle_btn, "clicked", G_CALLBACK(on_toggle_handle_clicked), NULL);
    gtk_box_pack_start(GTK_BOX(btn_row), g_handle_btn, FALSE, FALSE, 0);

    GtkWidget *delete_btn = gtk_button_new_with_label("Delete");
    GtkStyleContext *delete_style = gtk_widget_get_style_context(delete_btn);
    gtk_style_context_add_class(delete_style, "detail-btn");
    gtk_style_context_add_class(delete_style, "detail-btn-delete");
    g_signal_connect(delete_btn, "clicked", G_CALLBACK(on_delete_clicked), NULL);
    gtk_box_pack_start(GTK_BOX(btn_row), delete_btn, FALSE, FALSE, 0);

    gtk_box_pack_start(GTK_BOX(detail), btn_row, FALSE, FALSE, 0);

    /* Rebuild the sidebar from the server's own history + this machine's
     * local notes/flags/hidden state — see local_store.h and
     * http_fetch_history's doc comments for why both are needed. */
    LocalMeta *metas = NULL;
    int meta_count = 0;
    local_store_load(&metas, &meta_count);

    HistoryEntry *history = malloc(sizeof(HistoryEntry) * HISTORY_LIMIT);
    int hist_count = history ? http_fetch_history(&g_cfg, HISTORY_LIMIT, history, HISTORY_LIMIT) : -1;
    if (hist_count > 0) {
        /* The server returns newest-first; appending (position -1) in that
         * same order keeps the sidebar newest-on-top, matching how live
         * taps get prepended below. */
        for (int i = 0; i < hist_count; i++) {
            GuiEntry *e = entry_new(history[i].result.id, history[i].uid_hex, history[i].timestamp_ms, &history[i].result);
            if (!e) continue;
            apply_local_meta(e, metas, meta_count);
            if (e->hidden) {
                free(e);
                continue;
            }
            GtkWidget *row = build_row_widget(e);
            g_ptr_array_add(g_entries, e);
            gtk_list_box_insert(GTK_LIST_BOX(g_listbox), row, -1);
        }
    } else if (hist_count < 0) {
        fprintf(stderr, "couldn't fetch badge history from the server (starting with an empty sidebar)\n");
    }
    free(history);
    local_store_free(metas);

    /* Marks where history ends and this run's live taps begin — inserted
     * at the very top of what was just loaded (position 0). Live taps are
     * prepended above it (idle_new_tap), so it stays exactly on that
     * boundary as the session goes on, "so we can easily see when it
     * started" — not itself a GuiEntry, not selectable/clickable, just a
     * visual marker. */
    GtkWidget *sep_row = gtk_list_box_row_new();
    gtk_list_box_row_set_selectable(GTK_LIST_BOX_ROW(sep_row), FALSE);
    gtk_list_box_row_set_activatable(GTK_LIST_BOX_ROW(sep_row), FALSE);
    GtkWidget *sep_line = gtk_separator_new(GTK_ORIENTATION_HORIZONTAL);
    gtk_style_context_add_class(gtk_widget_get_style_context(sep_line), "session-marker");
    gtk_container_add(GTK_CONTAINER(sep_row), sep_line);
    gtk_list_box_insert(GTK_LIST_BOX(g_listbox), sep_row, 0);

    /* Newest history entry (if any) is g_entries[0] — history was appended
     * in the server's newest-first order. Select it up front so the detail
     * panel isn't blank before the first live tap of the session. */
    if (g_entries->len > 0) {
        GuiEntry *newest = g_ptr_array_index(g_entries, 0);
        gtk_list_box_select_row(GTK_LIST_BOX(g_listbox), GTK_LIST_BOX_ROW(newest->row));
    }

    gtk_widget_show_all(window);

    pthread_t thread;
    pthread_create(&thread, NULL, pcsc_thread_func, NULL);
    pthread_detach(thread);

    printf("badge-lookup-gui: API_BASE=%s, connecting...\n", g_cfg.api_base);
    lookup_client_connect(g_lc);

    gtk_main();
    return 0;
}
