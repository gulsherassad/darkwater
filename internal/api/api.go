package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/detect"
	"github.com/gulsherassad/darkwater/internal/tracker"
	"github.com/gulsherassad/darkwater/internal/vessels"
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

// Deps is everything the API needs. Optional fields may be nil.
type Deps struct {
	Store        *tracker.Store
	Hub          *Hub
	CoveragePath string
	Monitor      *detect.Monitor           // optional: dark-ship alerts
	Spoof        *detect.SpoofDetector     // optional
	Encounters   *detect.EncounterDetector // optional
	Vessels      *vessels.Registry         // optional: ship ID cards
}

// NewRouter maps URLs to handler functions.
func NewRouter(d Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/ships", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, toShipJSON(d.Store.Snapshot()))
	})

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		known := 0
		if d.Vessels != nil {
			known = d.Vessels.Len()
		}
		writeJSON(w, map[string]any{
			"status":     "ok",
			"ships":      d.Store.Len(),
			"idCards":    known,
			"detector":   d.Monitor != nil,
			"encounters": d.Encounters != nil,
		})
	})

	mux.HandleFunc("GET /api/ws", d.Hub.ServeWS(d.Store))

	mux.HandleFunc("GET /api/coverage", coverageHandler(d.CoveragePath))

	mux.HandleFunc("GET /api/alerts", func(w http.ResponseWriter, r *http.Request) {
		if d.Monitor == nil {
			writeJSON(w, []detect.Alert{})
			return
		}
		writeJSON(w, d.Monitor.Current())
	})

	mux.HandleFunc("GET /api/spoof", func(w http.ResponseWriter, r *http.Request) {
		if d.Spoof == nil {
			writeJSON(w, []detect.SpoofEvent{})
			return
		}
		writeJSON(w, d.Spoof.Recent())
	})

	mux.HandleFunc("GET /api/encounters", func(w http.ResponseWriter, r *http.Request) {
		if d.Encounters == nil {
			writeJSON(w, []detect.EncounterEvent{})
			return
		}
		writeJSON(w, d.Encounters.Recent())
	})

	// One ship's ID card, e.g. /api/vessels/219000001
	mux.HandleFunc("GET /api/vessels/{mmsi}", func(w http.ResponseWriter, r *http.Request) {
		mmsi, err := strconv.ParseInt(r.PathValue("mmsi"), 10, 64)
		if err != nil {
			http.Error(w, "bad mmsi", http.StatusBadRequest)
			return
		}
		if d.Vessels == nil {
			http.NotFound(w, r)
			return
		}
		info, ok := d.Vessels.Get(mmsi)
		if !ok {
			http.NotFound(w, r) // no ID card received yet
			return
		}
		writeJSON(w, info)
	})

	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
