package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"badgescanner/backend/internal/service"
)

func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.svc.AllUserRows()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	q := r.URL.Query()
	filters := service.UserFilters{
		Query:       q.Get("query"),
		Type:        q.Get("type"),
		ScannedOnly: q.Get("scannedOnly") == "true",
		ErrorOnly:   q.Get("errorOnly") == "true",
		Coalition:   q.Get("coalition"),
		Order:       q.Get("order"),
	}
	filtered := service.FilterUserRows(rows, filters)

	coalitions, err := a.svc.AvailableCoalitions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rows":       filtered,
		"coalitions": coalitions,
	})
}

func (a *API) getUser(w http.ResponseWriter, r *http.Request) {
	pk, err := strconv.ParseInt(chi.URLParam(r, "pk"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pk")
		return
	}
	detail, ok, err := a.svc.GetUserDetail(pk)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (a *API) deleteUser(w http.ResponseWriter, r *http.Request) {
	pk, err := strconv.ParseInt(chi.URLParam(r, "pk"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pk")
		return
	}
	if err := a.svc.DeleteUser(pk); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) refreshUserProfile(w http.ResponseWriter, r *http.Request) {
	pk, err := strconv.ParseInt(chi.URLParam(r, "pk"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pk")
		return
	}
	if err := a.svc.RefreshUserProfile(pk); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	detail, _, _ := a.svc.GetUserDetail(pk)
	writeJSON(w, http.StatusOK, detail)
}

func (a *API) refreshUserCoalition(w http.ResponseWriter, r *http.Request) {
	pk, err := strconv.ParseInt(chi.URLParam(r, "pk"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pk")
		return
	}
	if err := a.svc.RefreshUserCoalition(pk); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	detail, _, _ := a.svc.GetUserDetail(pk)
	writeJSON(w, http.StatusOK, detail)
}

func (a *API) addManualBlame(w http.ResponseWriter, r *http.Request) {
	pk, err := strconv.ParseInt(chi.URLParam(r, "pk"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pk")
		return
	}
	record, err := a.svc.AddManualBlame(pk)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, record)
}
