package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/tracker"
)

const (
	broadcastEvery = time.Second
	clientBuffer   = 16 // max queued messages per browser before we drop it
	writeTimeout   = 10 * time.Second
	maxClientMsg   = 1024 // browsers only send small viewport messages
)

var upgrader = websocket.Upgrader{}

// wsMessage is what the browser receives.
type wsMessage struct {
	Type  string     `json:"type"` // "snapshot" or "update"
	Ships []shipJSON `json:"ships"`
}

// viewportMsg is what the browser sends when its map view changes.
type viewportMsg struct {
	Type  string  `json:"type"`
	West  float64 `json:"west"`
	South float64 `json:"south"`
	East  float64 `json:"east"`
	North float64 `json:"north"`
}

func (m viewportMsg) valid() bool {
	return m.Type == "viewport" &&
		m.South >= -90 && m.North <= 90 && m.South < m.North &&
		m.West >= -180 && m.East <= 180 && m.West < m.East
}

// bbox is a rectangle on the map. A nil *bbox means "everything".
type bbox struct {
	West, South, East, North float64
}

func (b *bbox) contains(p ais.Position) bool {
	if b == nil {
		return true
	}
	return p.Lat >= b.South && p.Lat <= b.North && p.Lon >= b.West && p.Lon <= b.East
}

func filter(ps []ais.Position, view *bbox) []ais.Position {
	if view == nil {
		return ps
	}
	out := make([]ais.Position, 0, len(ps))
	for _, p := range ps {
		if view.contains(p) {
			out = append(out, p)
		}
	}
	return out
}

func marshalMsg(typ string, ps []ais.Position) ([]byte, error) {
	return json.Marshal(wsMessage{Type: typ, Ships: toShipJSON(ps)})
}

// client is one connected browser with its own mailbox and view.
type client struct {
	send chan []byte
	view atomic.Pointer[bbox] // written by readLoop, read by broadcast
}

// Hub tracks connected browsers and sends ship changes to them.
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

// deliverLocked puts msg in c's mailbox, or drops c if the mailbox is full.
// The caller must hold h.mu.
func (h *Hub) deliverLocked(c *client, msg []byte) {
	select {
	case c.send <- msg:
	default:
		delete(h.clients, c)
		close(c.send)
		log.Println("dropped slow client")
	}
}

// sendTo safely delivers to one client, if it's still connected.
func (h *Hub) sendTo(c *client, msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		h.deliverLocked(c, msg)
	}
}

// broadcast sends each client only the changed ships inside its view.
func (h *Hub) broadcast(changed []ais.Position) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		visible := filter(changed, c.view.Load())
		if len(visible) == 0 {
			continue
		}
		msg, err := marshalMsg("update", visible)
		if err != nil {
			log.Println("marshal: ", err)
			continue
		}
		h.deliverLocked(c, msg)
	}
}

// Run sends changes once per second until ctx is cancelled.
func (h *Hub) Run(ctx context.Context, store *tracker.Store) {
	ticker := time.NewTicker(broadcastEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if changed := store.TakeChanges(); len(changed) > 0 {
				h.broadcast(changed)
			}
		}
	}
}

// ServeWS upgrades an HTTP request to a WebSocket, sends a snapshot,
// then keeps the browser subscribed to updates for its view.
func (h *Hub) ServeWS(store *tracker.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return // Upgrade already wrote an error response
		}

		c := &client{send: make(chan []byte, clientBuffer)}

		// Full snapshot first (no view yet). The browser sends its view
		// right after connecting, and gets a filtered snapshot back.
		if snap, err := marshalMsg("snapshot", store.Snapshot()); err == nil {
			c.send <- snap
		}
		h.add(c)

		go writeLoop(conn, c)
		h.readLoop(conn, c, store) // blocks until the browser disconnects
		h.remove(c)
	}
}

// readLoop handles viewport messages from the browser.
func (h *Hub) readLoop(conn *websocket.Conn, c *client, store *tracker.Store) {
	conn.SetReadLimit(maxClientMsg) // a browser can't flood us with huge messages
	for {
		var m viewportMsg
		if err := conn.ReadJSON(&m); err != nil {
			return // disconnected or sent garbage
		}
		if !m.valid() {
			continue
		}
		view := &bbox{West: m.West, South: m.South, East: m.East, North: m.North}
		c.view.Store(view)

		// New view: send a fresh snapshot of just that area
		if msg, err := marshalMsg("snapshot", filter(store.Snapshot(), view)); err == nil {
			h.sendTo(c, msg)
		}
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
