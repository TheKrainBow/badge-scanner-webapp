package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"

	"badgescanner/backend/internal/auth"
	"badgescanner/backend/internal/service"
)

type lookupRequest struct {
	UIDHex string `json:"uidHex"`
}

var errRateLimited = errors.New("rate limit exceeded")

// lookupResponse is what a live lookup (HTTP or WS) actually sends back:
// the LookupResult plus the usage row's own id, so a client (the C GUI)
// can key local per-badge state (notes, "to handle") on a tap it JUST
// made the same way it would on one it learned about later via
// /api/lookup/history — same id either way, since both come from the same
// api_key_usage row.
type lookupResponse struct {
	service.LookupResult
	ID int64 `json:"id"`
}

// performLookup is the shared core behind both the one-shot POST /api/lookup
// and the persistent GET /api/lookup/ws — rate limiting, usage logging, and
// live-event broadcasting all happen exactly once here regardless of which
// transport a caller used.
func (a *API) performLookup(key *auth.APIKeyIdentity, uidHex string) (lookupResponse, error) {
	if key.RateLimitPerMinute > 0 {
		n, err := a.svc.Store.CountAPIKeyUsageSince(key.ID, time.Now().Add(-time.Minute).UnixMilli())
		if err == nil && n >= key.RateLimitPerMinute {
			return lookupResponse{}, errRateLimited
		}
	}
	if key.RateLimitPerHour > 0 {
		n, err := a.svc.Store.CountAPIKeyUsageSince(key.ID, time.Now().Add(-time.Hour).UnixMilli())
		if err == nil && n >= key.RateLimitPerHour {
			return lookupResponse{}, errRateLimited
		}
	}

	result, err := a.svc.Lookup(uidHex)
	if err != nil {
		return lookupResponse{}, err
	}

	// Best-effort usage record + live event — never fails the lookup itself
	// over a logging/broadcast error.
	id, err := a.svc.Store.RecordAPIKeyUsage(key.ID, uidHex, result.Found, result.Login, result.CoalitionName, result.CoalitionColor, result.CoalitionImageURL, result.PhotoURL)
	if err != nil {
		log.Printf("record api key usage for key %d: %v", key.ID, err)
	}
	if a.svc.Events != nil {
		a.svc.Events.LookupOccurred(key.Name, uidHex, result.Login, result.CoalitionName, result.CoalitionColor, result.Found)
	}

	return lookupResponse{LookupResult: result, ID: id}, nil
}

// lookup backs POST /api/lookup, one of two routes reachable by "lookup"-
// scope API keys (see api.go's route wiring and service.Lookup's doc
// comment). Unlike /api/scan (a trusted, session-authenticated route that
// echoes err.Error() straight to the caller), this endpoint is reachable by
// an external, non-session client — errors are logged server-side but never
// echoed verbatim, to avoid leaking internal detail to that caller.
func (a *API) lookup(w http.ResponseWriter, r *http.Request) {
	var req lookupRequest
	if err := decodeJSON(r, &req); err != nil || req.UIDHex == "" {
		writeError(w, http.StatusBadRequest, "uidHex is required")
		return
	}
	key := auth.APIKeyFromContext(r.Context())
	if key == nil {
		writeError(w, http.StatusUnauthorized, "invalid client credentials")
		return
	}
	result, err := a.performLookup(key, req.UIDHex)
	if errors.Is(err, errRateLimited) {
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}
	if err != nil {
		log.Printf("lookup %q: %v", req.UIDHex, err)
		writeError(w, http.StatusBadGateway, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

var lookupWSUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// lookupWS backs GET /api/lookup/ws — the persistent-connection counterpart
// to POST /api/lookup, for the C client: one WS handshake instead of a new
// HTTP connection per tap. Same auth (X-Client-Id/Secret on the handshake
// request, which a non-browser client can set, unlike a browser's WS API),
// same performLookup core, so rate limits/usage/events behave identically.
func (a *API) lookupWS(w http.ResponseWriter, r *http.Request) {
	key := auth.APIKeyFromContext(r.Context())
	if key == nil {
		writeError(w, http.StatusUnauthorized, "invalid client credentials")
		return
	}
	conn, err := lookupWSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("lookup ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	for {
		var req lookupRequest
		if err := conn.ReadJSON(&req); err != nil {
			return
		}
		if req.UIDHex == "" {
			_ = conn.WriteJSON(map[string]string{"error": "uidHex is required"})
			continue
		}
		result, err := a.performLookup(key, req.UIDHex)
		if errors.Is(err, errRateLimited) {
			_ = conn.WriteJSON(map[string]string{"error": "rate limit exceeded"})
			continue
		}
		if err != nil {
			log.Printf("lookup ws %q: %v", req.UIDHex, err)
			_ = conn.WriteJSON(map[string]string{"error": "lookup failed"})
			continue
		}
		if err := conn.WriteJSON(result); err != nil {
			return
		}
	}
}

// lookupHistory backs GET /api/lookup/history?limit=N — lets a "lookup"-
// scope key fetch its OWN past lookups (never another key's), so a client
// like the C badge-lookup GUI can rebuild its recent-badges view after a
// restart instead of losing everything when it closes. Reuses the same
// Store.ListAPIKeyUsage the admin-only per-key usage endpoint already
// calls (listAPIKeyUsage, apikeys_handlers.go) — the only difference here
// is whose key ID is used: the caller's own, from context, not a URL param
// an admin session picked.
func (a *API) lookupHistory(w http.ResponseWriter, r *http.Request) {
	key := auth.APIKeyFromContext(r.Context())
	if key == nil {
		writeError(w, http.StatusUnauthorized, "invalid client credentials")
		return
	}
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	usage, err := a.svc.Store.ListAPIKeyUsage(key.ID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load history")
		return
	}
	out := make([]apiKeyUsageOut, 0, len(usage))
	for _, u := range usage {
		out = append(out, apiKeyUsageOut{
			ID: u.ID, Timestamp: u.Timestamp, UIDHex: u.UIDHex, Found: u.Found, Login: u.Login,
			CoalitionName: u.CoalitionName, CoalitionColor: u.CoalitionColor,
			CoalitionImageURL: u.CoalitionImageURL, PhotoURL: u.PhotoURL,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
