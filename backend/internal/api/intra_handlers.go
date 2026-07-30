package api

import "net/http"

func (a *API) intraInfo(w http.ResponseWriter, r *http.Request) {
	info, err := a.svc.IntraBulkInfo()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (a *API) refreshIntraUsers(w http.ResponseWriter, r *http.Request) {
	count, err := a.svc.RefreshAllIntraUsers()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}

func (a *API) refreshCoalitions(w http.ResponseWriter, r *http.Request) {
	count, err := a.svc.RefreshAllCoalitions()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}
