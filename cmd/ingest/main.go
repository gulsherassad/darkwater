package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/joho/godotenv"

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
	cfg := ingest.Config{
		APIKey:    apiKey,
		BBoxes:    ingest.Denmark,
		RecordDir: "data/raw",
	}
	if err := ingest.Run(ctx, cfg, store); err != nil {
		log.Fatal(err)
	}
	log.Println("shutting down cleanly")
}
