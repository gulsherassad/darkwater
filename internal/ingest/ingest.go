package ingest

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"time"

	"github.com/gorilla/websocket"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/recorder"
	"github.com/gulsherassad/darkwater/internal/tracker"
)

const (
	streamURL   = "wss://stream.aisstream.io/v0/stream"
	reportEvery = 10 * time.Second
	readTimeout = 30 * time.Second // total silence this long = dead connection
)

// Denmark and the surrounding straits: [south-west corner, north-east corner] as [lat, lon]
var Denmark = [][][]float64{{{54.0, 7.0}, {58.5, 16.0}}}

type Config struct {
	APIKey    string
	BBoxes    [][][]float64
	RecordDir string
}

// Run ingests until ctx is cancelled, reconnecting whenever the stream drops.
func Run(ctx context.Context, cfg Config, store *tracker.Store) error {
	rec, err := recorder.New(cfg.RecordDir)
	if err != nil {
		return err
	}
	defer rec.Close()

	backoff := time.Second
	for {
		start := time.Now()
		err := runOnce(ctx, cfg, store, rec)
		if ctx.Err() != nil {
			return nil // Ctrl+C: clean shutdown
		}

		// Connection was healthy for a while: start backoff fresh
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		log.Printf("stream dropped: %v (reconnecting in %s)", err, backoff)

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil
		}
		backoff = min(backoff*2, time.Minute)
	}
}

// runOnce handles a single connection, from dial until it breaks.
func runOnce(ctx context.Context, cfg Config, store *tracker.Store, rec *recorder.Recorder) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, streamURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	sub := map[string]any{
		"APIKey":             cfg.APIKey,
		"BoundingBoxes":      cfg.BBoxes,
		"FilterMessageTypes": []string{"PositionReport"},
	}
	if err := conn.WriteJSON(sub); err != nil {
		return err
	}

	// Hang up on Ctrl+C. The done channel stops this goroutine when
	// runOnce returns, so reconnects don't leave old goroutines behind.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()

	rejected := map[string]int{}
	windowMsgs := 0
	var windowGaps []time.Duration
	lastReport := time.Now()

	for {
		// If nothing arrives for readTimeout, ReadMessage returns an error
		conn.SetReadDeadline(time.Now().Add(readTimeout))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		windowMsgs++

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

func median(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2].Round(time.Second)
}
