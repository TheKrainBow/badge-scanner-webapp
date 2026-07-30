// Package wshub is the backend's live-event broadcast hub: every dashboard
// browser tab connected to GET /api/events gets pushed lookup and
// bulk-refresh-progress events as they happen, instead of polling. The
// broadcast/client-map pattern here is the same one webapp/reader-agent
// (now removed) used for its own browser-facing WebSocket.
package wshub

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]bool
}

func New() *Hub {
	return &Hub{clients: map[*websocket.Conn]bool{}}
}

func (h *Hub) Add(c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = true
}

func (h *Hub) Remove(c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
	c.Close()
}

func (h *Hub) broadcast(v any) {
	body, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if err := c.WriteMessage(websocket.TextMessage, body); err != nil {
			c.Close()
			delete(h.clients, c)
		}
	}
}

// The methods below give *Hub the exact method set of service.EventSink —
// structural typing means service.Service.Events can hold a *Hub without
// either package importing the other.

func (h *Hub) LookupOccurred(keyName, uidHex, login, coalitionName, coalitionColor string, found bool) {
	h.broadcast(map[string]any{
		"type":           "lookup",
		"keyName":        keyName,
		"uidHex":         uidHex,
		"login":          login,
		"coalitionName":  coalitionName,
		"coalitionColor": coalitionColor,
		"found":          found,
		"timestamp":      time.Now().UnixMilli(),
	})
}

func (h *Hub) RefreshProgress(job string, current, total int, currentItem string) {
	h.broadcast(map[string]any{
		"type":        "progress",
		"job":         job,
		"current":     current,
		"total":       total,
		"currentItem": currentItem,
	})
}

func (h *Hub) RefreshComplete(job string, count int) {
	h.broadcast(map[string]any{"type": "refreshComplete", "job": job, "count": count})
}

func (h *Hub) RefreshError(job string, message string) {
	h.broadcast(map[string]any{"type": "refreshError", "job": job, "message": message})
}
