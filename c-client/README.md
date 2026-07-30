# badge-lookup — C badge-lookup clients (CLI + GUI)

Two native clients sharing the same core (`env.c`, `pcsc.c`, `httpc.c`,
`lookup_client.c`): a CLI (`badge-lookup`) and a GTK3 desktop GUI
(`badge-lookup-gui`). Both tap a physical PC/SC NFC badge reader directly
via the host's own `pcscd`, ask the backend "who is linked to this badge"
over a persistent WebSocket connection, and print a terminal log line —
the GUI additionally keeps a scrollable sidebar of recent badges you can
click through, annotate, and flag.

Both can only ever reach the `lookup`-scope routes (`POST /api/lookup`,
`GET /api/lookup/ws`, `GET /api/lookup/history`). The backend's scope
routing (see `../backend/internal/api/api.go`) and the narrow
`LookupResult` DTO (`../backend/internal/service/service.go`) make that
structural, not just a client-side convention: there is no TIG data, no
coalition points, and no blame information anywhere in that response,
regardless of what a client asks for.

**Rate limits**: each API key can have a per-minute/per-hour lookup limit
configured from its Admin page detail view. If this client hits one, the
backend replies with an `{"error": "rate limit exceeded"}` frame instead of
a result — printed to stderr, not treated as fatal.

## Build

### CLI (`make`)

Needs `pcscd` running on the host, dev headers for PC/SC and libcurl, and
**libcurl >= 7.86** (for `CURLOPT_CONNECT_ONLY`'s WebSocket mode — check
with `curl-config --version`; most current distro packages are well past
this):

```bash
# Debian/Ubuntu
sudo apt install libpcsclite-dev libcurl4-openssl-dev pcscd xdg-utils

# Fedora
sudo dnf install pcsc-lite-devel libcurl-devel pcsc-lite xdg-utils

sudo systemctl start pcscd   # if not already running
make
```

This vendors [cJSON](https://github.com/DaveGamble/cJSON) (MIT-licensed,
single-file) under `src/vendor/` — no other third-party dependency; the
WebSocket transport itself is libcurl's own (`curl_ws_send`/`curl_ws_recv`),
not a separate library.

### GUI (`make gui`, opt-in)

Same prerequisites as the CLI, plus GTK3 dev headers:

```bash
# Debian/Ubuntu
sudo apt install libgtk-3-dev

# Fedora
sudo dnf install gtk3-devel

make gui   # produces bin/badge-lookup-gui — plain `make`/`make all` never builds this
```

`make`/`make all` never require or touch GTK — the GUI is a separate,
explicit target so nobody who only wants the CLI needs to install it.

**PN532-based readers (e.g. the ACR122U)**: Linux's in-tree `pn533_usb` NFC
kernel driver auto-binds to these on plug-in and holds the USB interface
exclusively, which makes `pcscd` fail with `LIBUSB_ERROR_BUSY` and the
reader never shows up. If `lsmod | grep pn533` shows it loaded, free the
device for `pcscd`/this client:

```bash
sudo rmmod pn533_usb pn533
printf 'blacklist pn533_usb\nblacklist pn533\n' | sudo tee /etc/modprobe.d/blacklist-pn533.conf
```

The `rmmod` releases the device immediately; the blacklist file stops the
kernel from re-claiming it on the next replug or reboot.

## Configure and run

Both binaries read the same `.env`:

```bash
cp .env.example .env
# edit .env: API_BASE, and a "lookup"-scope API_CLIENT_ID/SECRET created
# from the webapp's Admin page → API Keys section (not the frontend's own
# "full"-scope key)
./bin/badge-lookup                  # CLI, reads ./.env by default
./bin/badge-lookup /path/to/other.env

./bin/badge-lookup-gui               # GUI, same .env convention
```

**⚠️ Don't run both against the same physical reader at once.** Nothing
stops you, but both would react to every tap independently (duplicate
lookups, duplicate history entries) — pick one per reader.

On startup both connect to `{API_BASE}/api/lookup/ws` (derived from
`API_BASE` — no separate WS URL to configure) and reconnect automatically,
5s backoff, if the connection ever drops. Every tap prints:

```
[DD/MM/YYYY] [HH:MM:SS] Badge tapped: heinz (Corrino)
```

(or `Badge tapped: unknown (<uidHex>)` if the badge isn't linked to
anyone) — the GUI prints this too, alongside its window, so whichever
terminal you launched it from keeps a running log either way.

The CLI additionally opens the photo in the OS's default viewer (saved to
`/tmp/badge-lookup-<uid>.<ext>`) on a match. The GUI shows the photo in its
own detail panel instead (lazily downloaded the first time you select that
badge in the sidebar, then cached).

### GUI specifics

- **Sidebar** (left, scrollable): every past lookup this API key has ever
  made, rebuilt from the server on each launch (`GET /api/lookup/history`)
  so closing and reopening the GUI doesn't lose anything — newest on top,
  live taps prepended as they happen.
- **Detail panel** (right): photo, login, coalition, a free-text note
  ("Save note"), a "Mark as to handle" toggle, and "Delete".
- **Red pin**: any badge marked "to handle" shows a red dot in the
  sidebar — clear it by toggling the button back to "Mark as handled".
- **Delete is local-only.** It hides that entry from *this machine's*
  sidebar (recorded in `~/.badge-lookup-gui.json`) and nothing else — the
  server's own history is untouched, so an admin auditing `/api/lookup/history`
  or the dashboard's History page still sees everything. Deleted entries
  stay hidden across restarts (the local file is what makes that stick,
  not just in-memory state).
- Notes and the "to handle" flag are also stored in that same local file,
  keyed by the server's own usage-row id — never sent to the backend.

## Layout

- `src/main.c` — CLI entrypoint: loads `.env`, connects, starts the PC/SC
  poll loop.
- `src/gui_main.c` — GTK3 GUI entrypoint: same connect/poll setup as the
  CLI, plus the sidebar/detail window, a background `pthread` running the
  PC/SC poll loop (GTK owns the main thread for its own event loop), and
  `g_idle_add` to safely hand new taps back to the main thread for display.
- `src/lookup_client.c`/`.h` — shared WS-connect/HTTP-fallback/reconnect
  logic (`LookupClient`), used by both entrypoints so they can't drift.
- `src/env.c`/`.h` — tiny `KEY=VALUE` `.env` parser.
- `src/pcsc.c`/`.h` — PC/SC polling loop (`SCardEstablishContext` →
  `SCardListReaders` → `SCardGetStatusChange` → `SCardConnect` +
  `SCardTransmit` with the standard "Get UID" pseudo-APDU
  `FF CA 00 00 00` → `SCardDisconnect`), debounced 2.5s per UID.
- `src/httpc.c`/`.h` — the persistent `ws_connect`/`ws_send_lookup`/
  `ws_recv_result`/`ws_close` WebSocket API (libcurl-native, see Build
  above) used by default, a plain `http_lookup` one-shot POST kept as a
  fallback/reference implementation, `http_fetch_history` (GUI-only, backs
  the sidebar rebuild), and a plain photo download.
- `src/display.c`/`.h` — `print_tap_line` (the shared terminal log line,
  used by both binaries) and, CLI-only, opening the photo via `xdg-open`
  (falls back to printing the saved path if `xdg-open` isn't installed,
  rather than failing silently).
- `src/local_store.c`/`.h` — GUI-only: the `~/.badge-lookup-gui.json`
  local-metadata file (notes/"to handle"/hidden), never sent to the server.
- `src/vendor/cJSON.{c,h}` — vendored JSON parser.
