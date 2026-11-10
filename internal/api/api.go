package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/tracker"
)

// shipJSON is what the frontend sees. It's separate from ais.Position
// so internal changes don't break the API.
type shipJSON struct {
	MMSI      int64     `json:"mmsi"`
	Name      string    `json:"name"`
	Time      time.Time `json:"time"`
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	SpeedKn   float64   `json:"speedKn"`   // -1 = unknown
	CourseDeg float64   `json:"courseDeg"` // -1 = unknown
	Heading   int       `json:"heading"`   // -1 = unknown
	NavStatus int       `json:"navStatus"`
}

func toShipJSON(ps []ais.Position) []shipJSON {
	out := make([]shipJSON, len(ps))
	for i, p := range ps {
		out[i] = shipJSON{
			MMSI:      p.MMSI,
			Name:      p.Name,
			Time:      p.Time,
			Lat:       p.Lat,
			Lon:       p.Lon,
			SpeedKn:   p.SpeedKn,
			CourseDeg: p.CourseDeg,
			Heading:   p.Heading,
			NavStatus: p.NavStatus,
		}
	}
	return out
}

// NewRouter maps URLs to handler functions.
func NewRouter(store *tracker.Store, hub *Hub) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/ships", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, toShipJSON(store.Snapshot()))
	})

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "ok", "ships": store.Len()})
	})

	mux.HandleFunc("GET /api/ws", hub.ServeWS(store))

	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
