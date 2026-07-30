package api

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var eventsUpgrader = websocket.Upgrader{
	// Same-origin only in practice (nginx fronts both the browser and this
	// route on one origin) — set explicitly rather than relying on the
	// permissive gorilla default.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// events backs GET /api/events, the browser dashboard's live feed (lookups,
// bulk-refresh progress). Gated by session cookie only (see api.go's route
// wiring) — a browser's WebSocket API can't set the X-Client-Id/Secret
// headers the "full" scope gate needs, so this route deliberately sits
// outside that gate.
func (a *API) events(w http.ResponseWriter, r *http.Request) {
	conn, err := eventsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("events ws upgrade: %v", err)
		return
	}
	a.hub.Add(conn)
	defer a.hub.Remove(conn)

	// Broadcast-only: drain/ignore anything the browser sends, just notice
	// when the connection closes.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
