#ifndef DISPLAY_H
#define DISPLAY_H

#include "httpc.h"

/* Extracts a photo URL's file extension (default "jpg" if none), stripping
 * any query string. Returns a pointer to an internal static buffer — NOT
 * thread-safe, and the result is only valid until the next call. Call only
 * from one thread (the GUI does this on its main thread only, when a row
 * is selected, never from its background PC/SC thread). */
const char *photo_extension(const char *url);

/* Prints "[DD/MM/YYYY] [HH:MM:SS] Badge tapped: <login> (<coalition>)" (or
 * "... Badge tapped: unknown (<uidHex>)" if not found) to stdout. Shared by
 * the CLI (display_result, below) and the GUI's background tap thread, so
 * both keep a terminal log even when the GUI has its own window. */
void print_tap_line(const char *uid_hex, const LookupResult *result);

/* Calls print_tap_line, then — CLI-only — if found and a photo URL is
 * present, downloads it to /tmp and opens it with the OS's default viewer
 * (xdg-open). The GUI shows photos in its own window instead, so it calls
 * print_tap_line directly rather than this. */
void display_result(const char *uid_hex, const LookupResult *result);

#endif
