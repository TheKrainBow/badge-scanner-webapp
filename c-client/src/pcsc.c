/* PC/SC polling loop — a C translation of ../../reader-agent/main.go's
 * runPCSC/readUID (see that file's comments for the "why" of the
 * carry-forward dwCurrentState/dwEventState trick, which avoids a
 * CPU-pegging busy loop instead of an actual blocking poll). */
#include "pcsc.h"

#if defined(__has_include)
#  if __has_include(<winscard.h>)
#    include <winscard.h>
#  elif __has_include(<PCSC/winscard.h>)
#    include <PCSC/winscard.h>
#  else
#    error "winscard.h not found — install libpcsclite-dev (Debian/Ubuntu) or pcsc-lite-devel (Fedora)"
#  endif
#else
#  include <winscard.h>
#endif

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#define POLL_INTERVAL_MS 500
#define DEBOUNCE_MS 2500

static const BYTE GET_UID_APDU[] = {0xFF, 0xCA, 0x00, 0x00, 0x00};

static long now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (long)ts.tv_sec * 1000 + ts.tv_nsec / 1000000;
}

static int read_uid(SCARDCONTEXT ctx, const char *reader, char *out_hex, size_t out_hex_len) {
    SCARDHANDLE hCard;
    DWORD dwProtocol;
    LONG rv = SCardConnect(ctx, reader, SCARD_SHARE_SHARED, SCARD_PROTOCOL_T0 | SCARD_PROTOCOL_T1, &hCard, &dwProtocol);
    if (rv != SCARD_S_SUCCESS) return -1;

    SCARD_IO_REQUEST pioSendPci;
    pioSendPci.dwProtocol = dwProtocol;
    pioSendPci.cbPciLength = sizeof(SCARD_IO_REQUEST);

    BYTE recvBuf[64];
    DWORD recvLen = sizeof(recvBuf);
    rv = SCardTransmit(hCard, &pioSendPci, GET_UID_APDU, sizeof(GET_UID_APDU), NULL, recvBuf, &recvLen);
    SCardDisconnect(hCard, SCARD_LEAVE_CARD);
    if (rv != SCARD_S_SUCCESS || recvLen < 2) return -1;

    if (recvBuf[recvLen - 2] != 0x90 || recvBuf[recvLen - 1] != 0x00) return -1;

    size_t uid_len = recvLen - 2;
    if (uid_len * 2 >= out_hex_len) return -1;
    for (size_t i = 0; i < uid_len; i++) {
        snprintf(out_hex + i * 2, 3, "%02X", recvBuf[i]);
    }
    return 0;
}

static int run_pcsc(pcsc_tap_callback cb) {
    SCARDCONTEXT ctx;
    LONG rv = SCardEstablishContext(SCARD_SCOPE_SYSTEM, NULL, NULL, &ctx);
    if (rv != SCARD_S_SUCCESS) {
        fprintf(stderr, "SCardEstablishContext: %s\n", pcsc_stringify_error(rv));
        return -1;
    }

    char last_uid[64] = "";
    long last_tap_at = 0;

    SCARD_READERSTATE state;
    int have_reader = 0;
    char readers[1024];
    char reader_name[sizeof(readers)] = "";

    for (;;) {
        if (!have_reader) {
            DWORD readers_len = sizeof(readers);
            rv = SCardListReaders(ctx, NULL, readers, &readers_len);
            if (rv != SCARD_S_SUCCESS || readers_len <= 1) {
                usleep(POLL_INTERVAL_MS * 1000);
                continue;
            }
            snprintf(reader_name, sizeof(reader_name), "%s", readers);
            memset(&state, 0, sizeof(state));
            state.szReader = reader_name;
            state.dwCurrentState = SCARD_STATE_UNAWARE;
            have_reader = 1;
        }

        rv = SCardGetStatusChange(ctx, POLL_INTERVAL_MS, &state, 1);
        if (rv == SCARD_E_UNKNOWN_READER || rv == SCARD_E_NO_READERS_AVAILABLE) {
            have_reader = 0;
            continue;
        }
        if (rv != SCARD_S_SUCCESS && (DWORD)rv != SCARD_E_TIMEOUT) {
            fprintf(stderr, "SCardGetStatusChange: %s\n", pcsc_stringify_error(rv));
            SCardReleaseContext(ctx);
            return -1;
        }

        /* Carry the reported state forward as next call's baseline — this
         * is what makes GetStatusChange actually block instead of firing
         * again immediately (see reader-agent/main.go's comment). */
        DWORD event_state = state.dwEventState;
        state.dwCurrentState = event_state;

        if (!(event_state & SCARD_STATE_PRESENT)) continue;

        char uid_hex[64];
        if (read_uid(ctx, reader_name, uid_hex, sizeof(uid_hex)) != 0) continue;

        long now = now_ms();
        if (strcmp(uid_hex, last_uid) == 0 && (now - last_tap_at) < DEBOUNCE_MS) continue;
        snprintf(last_uid, sizeof(last_uid), "%s", uid_hex);
        last_tap_at = now;

        cb(uid_hex);
    }
}

int pcsc_run_loop(pcsc_tap_callback cb) {
    for (;;) {
        int rc = run_pcsc(cb);
        if (rc != 0) {
            fprintf(stderr, "PC/SC error — retrying in 5s (is pcscd running and a reader plugged in?)\n");
            sleep(5);
        }
    }
}
