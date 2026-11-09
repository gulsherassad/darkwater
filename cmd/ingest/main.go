package main

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/tracker"
)

const streamURL = "wss://stream.aisstream.io/v0/stream"

func main() {
	godotenv.Load()
	apiKey := os.Getenv("AISSTREAM_API_KEY")
	if apiKey == "" {
		log.Fatal("AISSTREAM_API_KEY not set")
	}

	conn, _, err := websocket.DefaultDialer.Dial(streamURL, nil)
	if err != nil {
		log.Fatal("dial: ", err)
	}
	defer conn.Close()

	sub := map[string]any{
		"APIKey":             apiKey,
		"BoundingBoxes":      [][][]float64{{{54.0, 7.0}, {58.5, 16.0}}}, // Denmark
		"FilterMessageTypes": []string{"PositionReport"},
	}
	if err := conn.WriteJSON(sub); err != nil {
		log.Fatal("subscribe: ", err)
	}

	store := tracker.New()
	rejected := map[string]int{} // reason -> count
	accepted := 0
	lastReport := time.Now()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Fatal("read: ", err)
		}

		var env ais.Envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			rejected["bad_json"]++
			continue
		}

		pos, ok := env.ToPosition()
		if !ok {
			continue
		}

		if reason := pos.Validate(); reason != "" {
			rejected[reason]++
			continue
		}

		if !store.Update(pos) {
			rejected["stale"]++
			continue
		}
		accepted++

		// Summary every 10 seconds instead of printing every ship
		if time.Since(lastReport) >= 10*time.Second {
			log.Printf("ships=%d accepted=%d rejected=%v", store.Len(), accepted, rejected)
			lastReport = time.Now()
		}
	}
}
