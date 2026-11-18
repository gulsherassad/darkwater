package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/joho/godotenv"

	"github.com/gulsherassad/darkwater/internal/api"
	"github.com/gulsherassad/darkwater/internal/detect"
	"github.com/gulsherassad/darkwater/internal/ingest"
	"github.com/gulsherassad/darkwater/internal/liveness"
	"github.com/gulsherassad/darkwater/internal/reception"
	"github.com/gulsherassad/darkwater/internal/tracker"
)

const (
	receptionPath = "data/grid/reception-res6.csv"
	coveragePath  = "data/grid/cells-res6.csv"
	scanEvery     = 30 * time.Second
)

func main() {
	godotenv.Load()
	apiKey := os.Getenv("AISSTREAM_API_KEY")
	if apiKey == "" {
		log.Fatal("AISSTREAM_API_KEY not set")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	store := tracker.New()
	live := liveness.New(detect.MaxSilence + time.Hour)

	// 1) Receiver runs in the background, writing to the store
	cfg := ingest.Config{
		APIKey:    apiKey,
		BBoxes:    ingest.Denmark,
		RecordDir: "data/raw-server",
		Liveness:  live,
	}
	ingestDone := make(chan error, 1)
	go func() {
		ingestDone <- ingest.Run(ctx, cfg, store)
	}()

	// 2) Hub broadcasts ship changes to connected browsers once per second
	hub := api.NewHub()
	go hub.Run(ctx, store)

	// 3) Detector: re-scores every ship every 30 s (if the reception model exists)
	var monitor *detect.Monitor
	if model, err := reception.Load(receptionPath, 20); err != nil {
		log.Printf("detector off: %v (run cmd/reception to build the model)", err)
	} else {
		monitor = detect.NewMonitor(model, store, live.Alive)
		go monitor.Run(ctx, scanEvery, nil)
		log.Printf("detector on: scanning every %s", scanEvery)
	}

	// 4) Web server runs in the foreground
	srv := &http.Server{
		Addr:    ":8080",
		Handler: api.NewRouter(store, hub, coveragePath, monitor),
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
