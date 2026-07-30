CC ?= cc
CFLAGS ?= -O2 -Wall -Wextra
SRC := src/main.c src/env.c src/pcsc.c src/httpc.c src/display.c src/lookup_client.c src/vendor/cJSON.c
BIN := bin/badge-lookup

# libcurl: always via pkg-config.
CURL_CFLAGS := $(shell pkg-config --cflags libcurl)
CURL_LIBS := $(shell pkg-config --libs libcurl)

# PC/SC: Debian/Ubuntu's libpcsclite-dev ships a pkg-config file
# (libpcsclite.pc); some distros don't, so fall back to the conventional
# header location (/usr/include/PCSC) and -lpcsclite directly.
PCSC_CFLAGS := $(shell pkg-config --exists libpcsclite && pkg-config --cflags libpcsclite || echo -I/usr/include/PCSC)
PCSC_LIBS := $(shell pkg-config --exists libpcsclite && pkg-config --libs libpcsclite || echo -lpcsclite)

override CFLAGS += $(CURL_CFLAGS) $(PCSC_CFLAGS)
LDFLAGS += $(CURL_LIBS) $(PCSC_LIBS)

# GUI: opt-in (`make gui`), never built by plain `make`/`make all`, so
# nobody without GTK3 installed is affected by its existence.
GUI_BIN := bin/badge-lookup-gui
GUI_SRC := src/gui_main.c src/lookup_client.c src/local_store.c \
           src/env.c src/pcsc.c src/httpc.c src/display.c src/vendor/cJSON.c
GTK_CFLAGS := $(shell pkg-config --cflags gtk+-3.0 2>/dev/null)
GTK_LIBS := $(shell pkg-config --libs gtk+-3.0 2>/dev/null)

.PHONY: all build gui clean install

all: build

build: $(BIN)

$(BIN): $(SRC)
	@mkdir -p bin
	$(CC) $(CFLAGS) -o $(BIN) $(SRC) $(LDFLAGS)

gui:
	@pkg-config --exists gtk+-3.0 || (echo "GTK3 not found — install libgtk-3-dev (Debian/Ubuntu) or gtk3-devel (Fedora)"; exit 1)
	@mkdir -p bin
	$(CC) $(CFLAGS) $(GTK_CFLAGS) -o $(GUI_BIN) $(GUI_SRC) $(LDFLAGS) $(GTK_LIBS) -lpthread

clean:
	rm -rf bin

install: build
	install -m 755 $(BIN) /usr/local/bin/badge-lookup
