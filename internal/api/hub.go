package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/gulsherassad/darkwater/internal/tracker"
)

const (
	broadcastEvery = time.Second
	clientBuffer   = 16 // max queued messages per browser before we drop it
	writeTimeout   = 10 * time.Second
)

var upgrader = websocket.Upgrader{}

// wsMessage is what the browser receives.
type wsMessage struct {
	Type  string     `json:"type"` // "snapshot" or "update"
	Ships []shipJSON `json:"ships"`
}

// client is one connected browser with its own mailbox.
type client struct {
	send chan []byte
}

// Hub tracks connected browsers and broadcasts ship changes to them.
type Hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*client]struct{})}
}

func (h *Hub) add(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
}

// Broadcast drops msg in every mailbox. A full mailbox means the
// browser can't keep up, so it gets disconnected instead of slowing everyone down.
func (h *Hub) Broadcast(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- msg:
		default:
			delete(h.clients, c)
			close(c.send)
			log.Println("dropped slow client")
		}
	}
}

// Run sends changed ships to everyone once per second until ctx is cancelled.
func (h *Hub) Run(ctx context.Context, store *tracker.Store) {
	ticker := time.NewTicker(broadcastEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changed := store.TakeChanges()
			if len(changed) == 0 {
				continue
			}
			msg, err := json.Marshal(wsMessage{Type: "update", Ships: toShipJSON(changed)})
			if err != nil {
				log.Println("marshal: ", err)
				continue
			}
			h.Broadcast(msg)
		}
	}
}

// ServeWS upgrades an HTTP request to a WebSocket, sends a snapshot,
// then keeps the browser subscribed to updates.
func (h *Hub) ServeWS(store *tracker.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return // Upgrade already wrote an error response
		}

		c := &client{send: make(chan []byte, clientBuffer)}

		// Snapshot goes in the mailbox first, so it arrives before any update
		snap, _ := json.Marshal(wsMessage{Type: "snapshot", Ships: toShipJSON(store.Snapshot())})
		c.send <- snap
		h.add(c)

		go writeLoop(conn, c)
		readLoop(conn) // blocks until the browser disconnects
		h.remove(c)
	}
}

// writeLoop delivers mailbox messages to the browser.
func writeLoop(conn *websocket.Conn, c *client) {
	defer conn.Close()
	for msg := range c.send {
		conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

// readLoop only exists to notice when the browser disconnects.
func readLoop(conn *websocket.Conn) {
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
