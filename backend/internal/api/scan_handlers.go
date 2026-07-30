package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"badgescanner/backend/internal/store"
)

type scanRequest struct {
	UIDHex string `json:"uidHex"`
}

func (a *API) scan(w http.ResponseWriter, r *http.Request) {
	var req scanRequest
	if err := decodeJSON(r, &req); err != nil || req.UIDHex == "" {
		writeError(w, http.StatusBadRequest, "uidHex is required")
		return
	}
	outcome, err := a.svc.Scan(req.UIDHex)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, outcome)
}

type associateRequest struct {
	UIDHex string `json:"uidHex"`
	Login  string `json:"login"`
}

func (a *API) associateBadge(w http.ResponseWriter, r *http.Request) {
	var req associateRequest
	if err := decodeJSON(r, &req); err != nil || req.UIDHex == "" || req.Login == "" {
		writeError(w, http.StatusBadRequest, "uidHex and login are required")
		return
	}
	record, err := a.svc.AssociateBadge(req.UIDHex, req.Login)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (a *API) listHistory(w http.ResponseWriter, r *http.Request) {
	limit := 100
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			offset = n
		}
	}
	records, err := a.svc.Store.ListScanHistory(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, records)
}

type patchHistoryRequest struct {
	Reason      *string            `json:"reason,omitempty"`
	BlameStatus *store.BlameStatus `json:"blameStatus,omitempty"`
	TigDuration *string            `json:"tigDuration,omitempty"`
}

func (a *API) patchHistory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req patchHistoryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := a.svc.UpdateScan(id, req.Reason, req.BlameStatus, req.TigDuration); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	record, err := a.svc.Store.GetScanRecord(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "record not found")
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (a *API) deleteHistory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := a.svc.Store.DeleteScanRecord(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) clearHistory(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.Store.ClearHistory(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) caInfo(w http.ResponseWriter, r *http.Request) {
	info, err := a.svc.CADirectoryInfo()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (a *API) refreshCADirectory(w http.ResponseWriter, r *http.Request) {
	count, err := a.svc.RefreshCADirectory()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}
