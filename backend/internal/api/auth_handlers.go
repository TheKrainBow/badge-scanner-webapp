package api

import (
	"errors"
	"net/http"

	"badgescanner/backend/internal/auth"
	"badgescanner/backend/internal/store"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	u, err := a.svc.Store.GetUserByUsername(req.Username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if err := a.auth.IssueSession(w, u); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, accountView(u))
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	a.auth.ClearSession(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	id := auth.FromContext(r.Context())
	if id == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       id.UserID,
		"username": id.Username,
		"isAdmin":  id.IsAdmin,
	})
}
