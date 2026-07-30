// Package api wires the HTTP routes: user session auth (the dashboard's
// sole auth layer — no API key required, see auth.go's doc comment on why),
// the scoped API-key gate for external non-browser clients (the C
// badge-lookup client's "lookup" scope), and handlers delegating to
// internal/service.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"badgescanner/backend/internal/auth"
	"badgescanner/backend/internal/service"
	"badgescanner/backend/internal/wshub"
)

type API struct {
	svc  *service.Service
	auth *auth.Service
	hub  *wshub.Hub
}

func NewRouter(svc *service.Service, authSvc *auth.Service, hub *wshub.Hub) http.Handler {
	a := &API{svc: svc, auth: authSvc, hub: hub}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api", func(r chi.Router) {
		r.Post("/auth/login", a.login)
		r.Post("/auth/logout", a.logout)

		r.Group(func(r chi.Router) {
			r.Use(authSvc.RequireSession)

			r.Get("/auth/me", a.me)

			r.Post("/scan", a.scan)
			r.Post("/badges/associate", a.associateBadge)

			r.Get("/history", a.listHistory)
			r.Patch("/history/{id}", a.patchHistory)
			r.Delete("/history/{id}", a.deleteHistory)
			r.Delete("/history", a.clearHistory)

			r.Get("/users", a.listUsers)
			r.Get("/users/{pk}", a.getUser)
			r.Delete("/users/{pk}", a.deleteUser)
			r.Post("/users/{pk}/refresh-profile", a.refreshUserProfile)
			r.Post("/users/{pk}/refresh-coalition", a.refreshUserCoalition)
			r.Post("/users/{pk}/manual-blame", a.addManualBlame)

			r.Post("/coalitions/score", a.coalitionScore)
			r.Post("/tig", a.giveTig)

			r.Get("/clusters", a.getClusters)
			r.Post("/clusters/refresh-occupants", a.refreshOccupants)

			r.Get("/ca/info", a.caInfo)

			r.Group(func(r chi.Router) {
				r.Use(authSvc.RequireAdmin)
				r.Post("/ca/refresh", a.refreshCADirectory)
				r.Get("/admin/settings", a.getSettings)
				r.Put("/admin/settings", a.putSettings)
				r.Get("/admin/users", a.listAccounts)
				r.Post("/admin/users", a.createAccount)
				r.Delete("/admin/users/{id}", a.deleteAccount)
				r.Patch("/admin/users/{id}", a.patchAccount)

				r.Post("/intra/refresh", a.refreshIntraUsers)
				r.Get("/intra/info", a.intraInfo)
				r.Post("/coalitions/refresh", a.refreshCoalitions)

				r.Get("/admin/api-keys", a.listAPIKeys)
				r.Post("/admin/api-keys", a.createAPIKey)
				r.Get("/admin/api-keys/{id}", a.getAPIKey)
				r.Patch("/admin/api-keys/{id}", a.updateAPIKey)
				r.Delete("/admin/api-keys/{id}", a.deleteAPIKey)
				r.Get("/admin/api-keys/{id}/usage", a.listAPIKeyUsage)
				r.Get("/admin/api-keys/usage", a.listAllAPIKeyUsage)
			})
		})

		// Separate gate: the C badge-lookup client never logs in
		// (no user session), it only ever needs "lookup" scope to reach
		// these routes (POST for a one-shot lookup, GET+upgrade for the
		// persistent WS mode, GET for its own past lookups so it can
		// rebuild state after a restart). See service.Lookup's doc comment
		// for why this can't leak blame/TIG/points data even by future
		// accident.
		r.Group(func(r chi.Router) {
			r.Use(authSvc.RequireAPIKeyPermission("lookup"))
			r.Post("/lookup", a.lookup)
			r.Get("/lookup/ws", a.lookupWS)
			r.Get("/lookup/history", a.lookupHistory)
		})

		// The browser dashboard's live feed — gated by session cookie only,
		// same as every other browser route.
		r.Group(func(r chi.Router) {
			r.Use(authSvc.RequireSession)
			r.Get("/events", a.events)
		})
	})

	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
