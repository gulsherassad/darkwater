package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"sort"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/recorder"
	"github.com/gulsherassad/darkwater/internal/tracker"
)

const (
	streamURL   = "wss://stream.aisstream.io/v0/stream"
	reportEvery = 10 * time.Second
)

func main() {
	godotenv.Load()
	apiKey := os.Getenv("AISSTREAM_API_KEY")
	if apiKey == "" {
		log.Fatal("AISSTREAM_API_KEY not set")
	}

	// Ctrl+C now cancels this context instead of killing the program instantly
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

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

	rec, err := recorder.New("data/raw")
	if err != nil {
		log.Fatal("recorder: ", err)
	}
	defer rec.Close()

	// On Ctrl+C, close the connection so ReadMessage stops waiting
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	store := tracker.New()
	rejected := map[string]int{}
	windowMsgs := 0
	var windowGaps []time.Duration
	lastReport := time.Now()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				log.Println("shutting down cleanly")
				break
			}
			log.Fatal("read: ", err)
		}
		windowMsgs++

		// Record raw, before any cleaning
		if err := rec.Write(msg, time.Now()); err != nil {
			log.Println("record: ", err)
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

		gap, ok := store.Update(pos)
		if !ok {
			rejected["stale"]++
			continue
		}
		if gap > 0 {
			windowGaps = append(windowGaps, gap)
		}

		if time.Since(lastReport) >= reportEvery {
			elapsed := time.Since(lastReport).Seconds()
			log.Printf("ships=%d  rate=%.1f msg/s  median_gap=%s  rejected=%v",
				store.Len(), float64(windowMsgs)/elapsed, median(windowGaps), rejected)
			rec.Flush()
			windowMsgs = 0
			windowGaps = windowGaps[:0]
			lastReport = time.Now()
		}
	}
}

// median returns the middle value of the gaps seen in this window.
func median(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2].Round(time.Second)
}
