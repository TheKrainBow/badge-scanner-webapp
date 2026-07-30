package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"badgescanner/backend/internal/auth"
	"badgescanner/backend/internal/store"
)

var validAPIKeyScopes = map[string]bool{"full": true, "lookup": true}

type apiKeyOut struct {
	ID                 int64    `json:"id"`
	ClientID           string   `json:"clientId"`
	Name               string   `json:"name"`
	Permissions        []string `json:"permissions"`
	CreatedAt          int64    `json:"createdAt"`
	LastUsedAt         int64    `json:"lastUsedAt"`
	RateLimitPerMinute int      `json:"rateLimitPerMinute"`
	RateLimitPerHour   int      `json:"rateLimitPerHour"`
}

func apiKeyView(k store.APIKey) apiKeyOut {
	return apiKeyOut{
		ID:                 k.ID,
		ClientID:           k.ClientID,
		Name:               k.Name,
		Permissions:        k.Scopes(),
		CreatedAt:          k.CreatedAt,
		LastUsedAt:         k.LastUsedAt,
		RateLimitPerMinute: k.RateLimitPerMinute,
		RateLimitPerHour:   k.RateLimitPerHour,
	}
}

func (a *API) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := a.svc.Store.ListAPIKeys()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]apiKeyOut, 0, len(keys))
	for _, k := range keys {
		out = append(out, apiKeyView(k))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) getAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	k, err := a.svc.Store.GetAPIKeyByID(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "key not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiKeyView(k))
}

type createAPIKeyRequest struct {
	Name               string   `json:"name"`
	Permissions        []string `json:"permissions"`
	RateLimitPerMinute int      `json:"rateLimitPerMinute"`
	RateLimitPerHour   int      `json:"rateLimitPerHour"`
}

type createAPIKeyResponse struct {
	apiKeyOut
	ClientSecret string `json:"clientSecret"`
}

func validatePermissions(permissions []string) error {
	if len(permissions) == 0 {
		return errors.New("at least one permission is required")
	}
	for _, p := range permissions {
		if !validAPIKeyScopes[p] {
			return errors.New("unknown permission: " + p)
		}
	}
	return nil
}

// createAPIKey generates a random client-id + secret, hashes the secret the
// same way user passwords are hashed (auth.HashPassword), stores only the
// hash, and returns the plaintext secret exactly once in this response —
// the operator's only chance to copy it. It is never retrievable again.
func (a *API) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var req createAPIKeyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := validatePermissions(req.Permissions); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.RateLimitPerMinute < 0 || req.RateLimitPerHour < 0 {
		writeError(w, http.StatusBadRequest, "rate limits cannot be negative")
		return
	}

	clientID, err := randomHex(16)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate client id")
		return
	}
	secret, err := randomHex(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate client secret")
		return
	}
	hash, err := auth.HashPassword(secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	k, err := a.svc.Store.CreateAPIKey(clientID, hash, req.Name, strings.Join(req.Permissions, ","),
		req.RateLimitPerMinute, req.RateLimitPerHour)
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			writeError(w, http.StatusConflict, "client id collision, please retry")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, createAPIKeyResponse{apiKeyOut: apiKeyView(k), ClientSecret: secret})
}

type updateAPIKeyRequest struct {
	Name               string   `json:"name"`
	Permissions        []string `json:"permissions"`
	RateLimitPerMinute int      `json:"rateLimitPerMinute"`
	RateLimitPerHour   int      `json:"rateLimitPerHour"`
}

// updateAPIKey edits name/permissions/rate limits in place. Client id and
// secret are never editable here — a key with different credentials is a
// different key (mirroring how account passwords are reset via a dedicated
// action, not an in-place field edit).
func (a *API) updateAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req updateAPIKeyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := validatePermissions(req.Permissions); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.RateLimitPerMinute < 0 || req.RateLimitPerHour < 0 {
		writeError(w, http.StatusBadRequest, "rate limits cannot be negative")
		return
	}
	if err := a.svc.Store.UpdateAPIKey(id, req.Name, strings.Join(req.Permissions, ","),
		req.RateLimitPerMinute, req.RateLimitPerHour); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	k, err := a.svc.Store.GetAPIKeyByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apiKeyView(k))
}

func (a *API) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := a.svc.Store.DeleteAPIKey(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.svc.Store.DeleteAPIKeyUsage(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type apiKeyUsageOut struct {
	ID                int64  `json:"id"`
	Timestamp         int64  `json:"timestamp"`
	UIDHex            string `json:"uidHex"`
	Found             bool   `json:"found"`
	Login             string `json:"login,omitempty"`
	CoalitionName     string `json:"coalitionName,omitempty"`
	CoalitionColor    string `json:"coalitionColor,omitempty"`
	CoalitionImageURL string `json:"coalitionImageUrl,omitempty"`
	PhotoURL          string `json:"photoUrl,omitempty"`
}

func (a *API) listAPIKeyUsage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	usage, err := a.svc.Store.ListAPIKeyUsage(id, 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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

type apiKeyUsageEntryOut struct {
	apiKeyUsageOut
	Badger string `json:"badger"`
}

// listAllAPIKeyUsage backs the History page: every lookup across every key,
// newest first.
func (a *API) listAllAPIKeyUsage(w http.ResponseWriter, r *http.Request) {
	usage, err := a.svc.Store.ListAllAPIKeyUsage(500)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]apiKeyUsageEntryOut, 0, len(usage))
	for _, u := range usage {
		out = append(out, apiKeyUsageEntryOut{
			apiKeyUsageOut: apiKeyUsageOut{
				ID: u.ID, Timestamp: u.Timestamp, UIDHex: u.UIDHex, Found: u.Found, Login: u.Login,
				CoalitionName: u.CoalitionName, CoalitionColor: u.CoalitionColor,
				CoalitionImageURL: u.CoalitionImageURL, PhotoURL: u.PhotoURL,
			},
			Badger: u.APIKeyName,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
