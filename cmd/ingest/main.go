package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"

	"github.com/gulsherassad/darkwater/internal/ais"
)

const streamURL = "wss://stream.aisstream.io/v0/stream"

func main() {
	// Load the API key from .env
	godotenv.Load()
	apiKey := os.Getenv("AISSTREAM_API_KEY")
	if apiKey == "" {
		log.Fatal("AISSTREAM_API_KEY not set")
	}

	// Dial: open the phone call
	conn, _, err := websocket.DefaultDialer.Dial(streamURL, nil)
	if err != nil {
		log.Fatal("dial: ", err)
	}
	defer conn.Close()

	// Say the password and what we want to hear (within 3 seconds)
	sub := map[string]any{
		"APIKey":             apiKey,
		"BoundingBoxes":      [][][]float64{{{54.0, 7.0}, {58.5, 16.0}}}, // Denmark
		"FilterMessageTypes": []string{"PositionReport"},
	}
	if err := conn.WriteJSON(sub); err != nil {
		log.Fatal("subscribe: ", err)
	}

	// Listen forever: read, translate, print
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Fatal("read: ", err)
		}

		var env ais.Envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			log.Println("bad json: ", err)
			continue
		}

		pos, ok := env.ToPosition()
		if !ok {
			continue // not a position report, skip it
		}

		log.Printf("%-20s %9d  %.4f,%.4f  %4.1f kn  status=%d",
			pos.Name, pos.MMSI, pos.Lat, pos.Lon, pos.SpeedKn, pos.NavStatus)
	}
}
