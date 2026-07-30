#ifndef PCSC_H
#define PCSC_H

/* Called with the tapped badge's UID as uppercase hex, once per tap
 * (debounced — see pcsc.c). */
typedef void (*pcsc_tap_callback)(const char *uid_hex);

/* Blocks forever, polling for a PC/SC reader and dispatching taps to cb.
 * Never returns under normal operation; returns nonzero only on a fatal
 * setup error (e.g. SCardEstablishContext itself failing). */
int pcsc_run_loop(pcsc_tap_callback cb);

#endif
