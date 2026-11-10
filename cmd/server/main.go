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
	"github.com/gulsherassad/darkwater/internal/ingest"
	"github.com/gulsherassad/darkwater/internal/tracker"
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

	// 1) Receiver runs in the background, writing to the store
	cfg := ingest.Config{
		APIKey:    apiKey,
		BBoxes:    ingest.Denmark,
		RecordDir: "data/raw",
	}
	ingestDone := make(chan error, 1)
	go func() {
		ingestDone <- ingest.Run(ctx, cfg, store)
	}()

	// 2) Web server runs in the foreground, reading from the store
	srv := &http.Server{
		Addr:    ":8080",
		Handler: api.NewRouter(store),
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

	// Wait for the receiver to finish cleanly (flushes the recorder)
	if err := <-ingestDone; err != nil {
		log.Println("ingest: ", err)
	}
	log.Println("shutting down cleanly")
}
