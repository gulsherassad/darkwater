package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/joho/godotenv"

	"github.com/gulsherassad/darkwater/internal/api"
	"github.com/gulsherassad/darkwater/internal/detect"
	"github.com/gulsherassad/darkwater/internal/ingest"
	"github.com/gulsherassad/darkwater/internal/liveness"
	"github.com/gulsherassad/darkwater/internal/reception"
	"github.com/gulsherassad/darkwater/internal/recorder"
	"github.com/gulsherassad/darkwater/internal/tracker"
	"github.com/gulsherassad/darkwater/internal/vessels"
)

const (
	receptionPath = "data/grid/reception-res6.csv"
	coveragePath  = "data/grid/cells-res6.csv"
	portsPath     = "data/grid/ports.csv"
	eventLogDir   = "data/alerts"
	scanEvery     = 30 * time.Second
	encounterScan = time.Minute
)

// eventLog lets several goroutines write to one log file safely.
type eventLog struct {
	mu  sync.Mutex
	rec *recorder.Recorder
}

func (l *eventLog) write(v any, t time.Time) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rec.Write(b, t)
	l.rec.Flush() // events are rare: write each one immediately
}

func main() {
	watchAlpha := flag.Float64("watch", 0.01, "flag as watch below this silence chance")
	alertAlpha := flag.Float64("alert", 0.001, "flag as alert below this silence chance")
	flag.Parse()

	godotenv.Load()
	apiKey := os.Getenv("AISSTREAM_API_KEY")
	if apiKey == "" {
		log.Fatal("AISSTREAM_API_KEY not set")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	store := tracker.New()
	ships := vessels.New()
	live := liveness.New(detect.MaxSilence + time.Hour)

	rec, err := recorder.New(eventLogDir)
	if err != nil {
		log.Fatal(err)
	}
	defer rec.Close()
	events := &eventLog{rec: rec}

	// Spoofing: every accepted position is checked against the ship's previous one
	spoof := detect.NewSpoofDetector()
	spoof.OnEvent = func(e detect.SpoofEvent) {
		events.write(e, e.Time)
		log.Printf("spoof: %-14s %-20s %9d  %.1f km in %.0f s (%.0f kn)",
			e.Kind, e.Name, e.MMSI, e.Km, e.Seconds, e.ImpliedKn)
	}

	// 1) Receiver runs in the background, writing to the store and the ID-card registry
	cfg := ingest.Config{
		APIKey:     apiKey,
		BBoxes:     ingest.Denmark,
		RecordDir:  "data/raw-server",
		Liveness:   live,
		OnPosition: spoof.Observe,
		OnStatic:   ships.Update,
	}
	ingestDone := make(chan error, 1)
	go func() {
		ingestDone <- ingest.Run(ctx, cfg, store)
	}()

	// 2) Hub broadcasts ship changes and alerts to connected browsers
	hub := api.NewHub()
	go hub.Run(ctx, store)

	// 3) Dark-ship detector: re-scores every ship every 30 s (if the reception model exists)
	var monitor *detect.Monitor
	if model, err := reception.Load(receptionPath, 20); err != nil {
		log.Printf("detector off: %v (run cmd/reception to build the model)", err)
	} else {
		monitor = detect.NewMonitor(model, store, live.Alive)
		monitor.WatchAlpha, monitor.AlertAlpha = *watchAlpha, *alertAlpha
		monitor.OnEvent = func(e detect.Event) {
			events.write(e, e.Time)
			log.Printf("event: %-10s %-20s %9d  %s  silent %d min", e.Kind, e.Name, e.MMSI, e.Level, e.SilentMinutes)
		}
		go monitor.Run(ctx, scanEvery, hub.BroadcastAlerts)
		log.Printf("detector on: scanning every %s (watch < %g, alert < %g)", scanEvery, *watchAlpha, *alertAlpha)
	}

	// 4) Encounter detector: scans once a minute (needs the port mask, or ports flood it)
	var enc *detect.EncounterDetector
	if mask, err := detect.LoadPortMask(portsPath); err != nil {
		log.Printf("encounters off: %v (run cmd/encscan to build the port mask)", err)
	} else {
		enc = detect.NewEncounterDetector()
		enc.Masked = mask.Masked
		enc.Info = ships.Get
		enc.OnEvent = func(e detect.EncounterEvent) {
			events.write(e, e.Time)
			log.Printf("encounter: %-15s %s + %s  %.0f min  draught %.1f/%.1f m",
				e.Kind, e.Name1, e.Name2, e.Minutes, e.Draught1, e.Draught2)
		}
		go func() {
			t := time.NewTicker(encounterScan)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					enc.Scan(time.Now().UTC(), store.Snapshot())
				}
			}
		}()
		log.Printf("encounters on: scanning every %s", encounterScan)
	}

	// 5) Web server runs in the foreground
	srv := &http.Server{
		Addr: ":8080",
		Handler: api.NewRouter(api.Deps{
			Store:        store,
			Hub:          hub,
			CoveragePath: coveragePath,
			Monitor:      monitor,
			Spoof:        spoof,
			Encounters:   enc,
			Vessels:      ships,
		}),
	}

	// On Ctrl+C: stop accepting requests, give in-flight ones 5s to finish
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Println("listening on http://localhost:8080")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}

	if err := <-ingestDone; err != nil {
		log.Println("ingest: ", err)
	}
	log.Println("shutting down cleanly")
}
