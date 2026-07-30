#include "display.h"

#include <stdio.h>
#include <string.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

const char *photo_extension(const char *url) {
    const char *slash = strrchr(url, '/');
    const char *search_from = slash ? slash : url;
    const char *dot = strrchr(search_from, '.');
    if (!dot) return "jpg";
    /* Strip a trailing query string, e.g. ".jpg?v=2" -> "jpg". */
    static char ext[16];
    size_t i = 0;
    for (const char *p = dot + 1; *p && *p != '?' && *p != '&' && i < sizeof(ext) - 1; p++, i++) {
        ext[i] = *p;
    }
    ext[i] = '\0';
    return ext[0] ? ext : "jpg";
}

/* Single fork+waitpid: xdg-open detaches the real viewer process itself and
 * exits quickly, so waiting for it is not the same as waiting for the
 * viewer to close. This also lets us detect "xdg-open not found" instead
 * of failing silently. */
static int open_in_viewer(const char *path) {
    pid_t pid = fork();
    if (pid < 0) return -1;
    if (pid == 0) {
        execlp("xdg-open", "xdg-open", path, (char *)NULL);
        _exit(127); /* execlp only returns on failure */
    }
    int status = 0;
    if (waitpid(pid, &status, 0) < 0) return -1;
    if (WIFEXITED(status) && WEXITSTATUS(status) == 127) return -1;
    return 0;
}

void print_tap_line(const char *uid_hex, const LookupResult *result) {
    char date_buf[16], time_buf[16];
    time_t now = time(NULL);
    struct tm tm_now;
    localtime_r(&now, &tm_now);
    strftime(date_buf, sizeof(date_buf), "%d/%m/%Y", &tm_now);
    strftime(time_buf, sizeof(time_buf), "%H:%M:%S", &tm_now);

    if (!result->found) {
        printf("[%s] [%s] Badge tapped: unknown (%s)\n", date_buf, time_buf, uid_hex);
        return;
    }
    if (result->coalition_name[0]) {
        printf("[%s] [%s] Badge tapped: %s (%s)\n", date_buf, time_buf, result->login, result->coalition_name);
    } else {
        printf("[%s] [%s] Badge tapped: %s\n", date_buf, time_buf, result->login);
    }
}

void display_result(const char *uid_hex, const LookupResult *result) {
    print_tap_line(uid_hex, result);
    if (!result->found) return;

    if (result->photo_url[0]) {
        char dest[300];
        snprintf(dest, sizeof(dest), "/tmp/badge-lookup-%s.%s", uid_hex, photo_extension(result->photo_url));
        if (http_download_photo(result->photo_url, dest) == 0) {
            if (open_in_viewer(dest) != 0) {
                printf("photo saved to %s (couldn't launch xdg-open — is xdg-utils installed?)\n", dest);
            }
        } else {
            fprintf(stderr, "failed to download photo from %s\n", result->photo_url);
        }
    }
}
