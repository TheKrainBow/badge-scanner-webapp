package api

import (
	"net/http"

	"badgescanner/backend/internal/intraclient"
)

type coalitionScoreRequest struct {
	PK     int64  `json:"pk"`
	Value  int    `json:"value"`
	Reason string `json:"reason"`
}

func (a *API) coalitionScore(w http.ResponseWriter, r *http.Request) {
	var req coalitionScoreRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	msg, err := a.svc.GiveCoalitionPoints(req.PK, req.Value, req.Reason)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": msg})
}

type tigRequest struct {
	PK              int64  `json:"pk"`
	DurationSeconds int    `json:"durationSeconds"`
	Reason          string `json:"reason"`
}

func (a *API) giveTig(w http.ResponseWriter, r *http.Request) {
	var req tigRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !intraclient.ValidTigDuration(req.DurationSeconds) {
		writeError(w, http.StatusBadRequest, "durationSeconds must be one of 7200, 14400, 28800")
		return
	}
	msg, err := a.svc.GiveTig(req.PK, req.DurationSeconds, req.Reason)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": msg})
}
