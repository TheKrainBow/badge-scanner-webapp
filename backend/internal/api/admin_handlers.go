package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"badgescanner/backend/internal/auth"
	"badgescanner/backend/internal/store"
)

type accountOut struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	IsAdmin   bool   `json:"isAdmin"`
	CreatedAt int64  `json:"createdAt"`
}

func accountView(u store.User) accountOut {
	return accountOut{ID: u.ID, Username: u.Username, IsAdmin: u.IsAdmin, CreatedAt: u.CreatedAt}
}

func (a *API) listAccounts(w http.ResponseWriter, r *http.Request) {
	users, err := a.svc.Store.ListUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]accountOut, 0, len(users))
	for _, u := range users {
		out = append(out, accountView(u))
	}
	writeJSON(w, http.StatusOK, out)
}

type createAccountRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	IsAdmin  bool   `json:"isAdmin"`
}

// createAccount is the only way to add users beyond the env-bootstrapped
// admin — no public signup, per the requested "admin creates the rest" model.
func (a *API) createAccount(w http.ResponseWriter, r *http.Request) {
	var req createAccountRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "username is required and password must be at least 8 characters")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	u, err := a.svc.Store.CreateUser(req.Username, hash, req.IsAdmin)
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			writeError(w, http.StatusConflict, "username already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, accountView(u))
}

func (a *API) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	caller := auth.FromContext(r.Context())
	if caller != nil && caller.UserID == id {
		writeError(w, http.StatusBadRequest, "cannot delete your own account")
		return
	}
	if err := a.svc.Store.DeleteUser(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type patchAccountRequest struct {
	Password *string `json:"password,omitempty"`
	IsAdmin  *bool   `json:"isAdmin,omitempty"`
}

func (a *API) patchAccount(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req patchAccountRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Password != nil {
		if len(*req.Password) < 8 {
			writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := a.svc.Store.SetUserPassword(id, hash); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.IsAdmin != nil {
		if err := a.svc.Store.SetUserAdmin(id, *req.IsAdmin); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	u, err := a.svc.Store.GetUserByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, accountView(u))
}

func (a *API) getSettings(w http.ResponseWriter, r *http.Request) {
	s, err := a.svc.Store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (a *API) putSettings(w http.ResponseWriter, r *http.Request) {
	var s store.AppSettings
	if err := decodeJSON(r, &s); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := a.svc.Store.SaveSettings(s); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}
