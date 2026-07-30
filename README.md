# Badge Scanner — Webapp

Desktop counterpart to the Android app in `../mobile`: same badge → Wiegand
→ CA → 42 intranet flow, same CA/42 fetch semantics and caching rules, but
backed by a shared Go server (SQLite) with real user accounts instead of
per-device storage, and a USB NFC reader instead of a phone's NFC radio.

## Layout

- `backend/` — Go REST API: CA/42 clients, SQLite storage, auth.
- `frontend/` — React + TypeScript (Vite) webapp. The Scan page takes a
  badge UID via manual hex entry — there's no browser-side physical-reader
  bridge (see below).
- `c-client/` — standalone C CLI: reads a badge directly via PC/SC and asks
  the backend "who is this" via a restricted `lookup`-scope API key — see
  `c-client/README.md`. This is the only component with direct physical
  reader support.

## How the pieces fit together

```
                                              backend (Go + SQLite)
                                                   │
                                          ┌────────┴────────┐
                                          ▼                 ▼
                                    CA (ibox4)         42 API (intra)
                                          ▲
                                          │ HTTPS + client-id/secret
                                          │ + session cookie
                       ┌──────────────────┴──────────────────┐
                       │                                     │
                 browser (frontend)                  c-client (PC/SC → lookup)
              manual badge UID entry                  USB reader (PC/SC)
```

> Note: an earlier iteration of this webapp had a `reader-agent` component
> bridging a physical USB reader to the browser's Scan page over a loopback
> WebSocket (browsers can't talk to PC/SC directly). It was removed —
> the Scan page's blame/TIG/coalition-points workflow now has no physical
> badge-tap input, only manual UID entry. `c-client/` reads the physical
> reader but is deliberately restricted to lookups only (see its README).

## Auth model (as requested)

1. **User accounts** — username + bcrypt password, JWT session cookie. This
   is the dashboard's *entire* auth layer — no API key required anywhere in
   the browser. On first run, if no users exist yet, the backend creates
   one admin from `ADMIN_USERNAME` / `ADMIN_PASSWORD` env vars. Every other
   account is created from the Admin page by an existing admin — there's no
   public signup.

   (An earlier iteration also gated every route, including login, behind a
   static shared-secret-turned-API-key. That's gone: a key deletable from
   this same dashboard's own Admin page was a pure self-lockout risk —
   delete it and you lock the whole dashboard out of its own backend, Admin
   page included — with no real security benefit over the session cookie
   already doing the actual authenticating.)

2. **Scoped API keys** — for external, non-browser clients only, which
   *can* set arbitrary headers on a request the way a browser's own
   fetch/WebSocket calls can't. Every request to a key-gated route must
   carry `X-Client-Id` / `X-Client-Secret` headers matching a row in the
   `api_keys` table, created from the Admin page's **API Keys** section
   (secret shown once at creation time). Scopes:
   - `lookup` — only `POST /api/lookup` / `GET /api/lookup/ws` (badge →
     login/coalition/photo, nothing else — no TIG, no coalition points, no
     blame). This is the scope given to the `c-client/` CLI's key — the
     scope restriction itself *is* the security boundary here,
     `/api/lookup`'s response is a hand-picked allowlist that structurally
     cannot include blame/TIG/points data (see
     `backend/internal/service/service.go`'s `Lookup`).
   - `full` — everything a session could reach. Not used by the dashboard;
     only useful if you want some other external automation client with
     broad access. Optionally pre-provisioned at first boot from
     `BOOTSTRAP_API_KEY_ID` / `BOOTSTRAP_API_KEY_SECRET` env vars if you
     want that available immediately instead of creating it later.

## Running it

### Docker Compose (backend + frontend)

The quickest way to run the backend and frontend together.

```bash
cd webapp
cp .env.example .env
# edit .env: set JWT_SECRET, ADMIN_USERNAME/PASSWORD (BOOTSTRAP_API_KEY_ID/SECRET are optional, see auth model above)
docker compose up -d --build
```

This builds and starts two containers:

- `backend` — the Go server, SQLite data persisted in the `backend-data`
  named volume (survives `docker compose down`; use `down -v` to wipe it).
- `frontend` — an nginx container serving the built static assets and
  reverse-proxying `/api/` and `/health` to the backend service. The
  browser only ever talks to this one origin, so the session cookie works
  with no CORS setup — `VITE_API_BASE` is baked in as `""` (relative) for
  exactly this reason.

Once it's up, open `http://localhost:8000` (or `$FRONTEND_PORT`), log in as
the bootstrapped admin, and set the CA/42 credentials from the Admin page.

To rebuild after changing backend code: `docker compose up -d --build`.

### Backend (without Docker)

```bash
cd backend
go build -o badgescanner-server ./cmd/server
JWT_SECRET=$(openssl rand -hex 32) \
  ADMIN_USERNAME=admin ADMIN_PASSWORD=... \
  ./badgescanner-server
```

Env vars:

| Var | Required | Notes |
|---|---|---|
| `JWT_SECRET` | yes | signs session cookies; any long random string |
| `BOOTSTRAP_API_KEY_ID` / `BOOTSTRAP_API_KEY_SECRET` | no | optional, see auth model above — the dashboard doesn't need this |
| `ADMIN_USERNAME` / `ADMIN_PASSWORD` | only until the first user exists | bootstraps the one hardcoded admin |
| `DB_PATH` | no (default `badgescanner.db`) | SQLite file path |
| `LISTEN_ADDR` | no (default `:8080`) | |
| `COOKIE_SECURE` | no (default `true`) | set to `false` only for plain-HTTP local dev — browsers won't store a `Secure` cookie over HTTP |

Once running, log in as the bootstrapped admin and set the CA/42
credentials, closer id, and campus id from the **Admin** page (these
replace the mobile app's per-device Settings screen — one shared
configuration for every operator now).

`go test ./...` runs the ported-logic tests (Wiegand, cluster SVG parser,
piscine login parsing — verified against the same fixtures as the Kotlin
unit tests).

### Frontend (without Docker)

Needs Node.js (developed against Node 20) — not installed on this machine;
install it first (`apt install nodejs npm`, or a version manager).

```bash
cd frontend
cp .env.example .env   # fill in VITE_API_BASE
npm install
npm run dev             # dev server, http://localhost:5173
npm run build            # type-checks + production build to dist/
```

### C badge-lookup client

For direct physical-reader support, see `c-client/README.md` — it's a
separate, restricted-scope tool (badge → login/coalition/photo only), not
a replacement for the Scan page's blame/TIG/points workflow, which now only
takes manual UID entry.

## What's ported 1:1 from the Android app

See the plan/commit history for the full list, but the load-bearing bits:
Wiegand candidate codes (`wiegand26`/unpadded/premium), the CA's paginated
user listing + `isListable` filtering + TLS-pinned self-signed cert, the
42 API's `client_credentials` token cache + 429 backoff + 404-as-empty
handling for coalitions, the 12h intra cache TTL, "CA directory only
refreshes on demand and clears manual badge links when it does", the
cluster SVG's two transform styles, and the coalition-pick priority order
(harkonnen/corrino/atreides > hordes/alliance > highest score).
