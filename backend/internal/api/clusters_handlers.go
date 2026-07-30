package api

import "net/http"

func (a *API) getClusters(w http.ResponseWriter, r *http.Request) {
	settings, err := a.svc.Store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	force := r.URL.Query().Get("force") == "true"
	data, err := a.svc.LoadClusters(settings.CampusID, force)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (a *API) refreshOccupants(w http.ResponseWriter, r *http.Request) {
	settings, err := a.svc.Store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	occupants, err := a.svc.RefreshClusterOccupants(settings.CampusID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, occupants)
}
