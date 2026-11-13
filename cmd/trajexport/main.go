package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/gulsherassad/darkwater/internal/features"
	"github.com/gulsherassad/darkwater/internal/track"
)

const outDir = "data/ml"

var horizons = []int64{5 * 60, 15 * 60, 30 * 60, 60 * 60}

const (
	valAfterHours  = 15 // 15:00–18:00 = validation
	testAfterHours = 18 // 18:00–24:00 = test (same as trajeval)
	minSpeedKn     = 3
	truthSlack     = 60
)

type splitWriter struct {
	f    *os.File
	w    *bufio.Writer
	rows int
}

func newSplit(name string) *splitWriter {
	f, err := os.Create(filepath.Join(outDir, name+".bin"))
	if err != nil {
		log.Fatal(err)
	}
	return &splitWriter{f: f, w: bufio.NewWriterSize(f, 1<<20)}
}

func (s *splitWriter) write(row []float32) {
	if err := binary.Write(s.w, binary.LittleEndian, row); err != nil {
		log.Fatal(err)
	}
	s.rows++
}

func (s *splitWriter) close() {
	if err := s.w.Flush(); err != nil {
		log.Fatal(err)
	}
	s.f.Close()
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/trajexport <dma.csv>")
		os.Exit(2)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatal(err)
	}

	tracks, err := track.LoadDMA(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	var minT int64 = -1
	for _, tr := range tracks {
		if minT < 0 || tr.Points[0].T < minT {
			minT = tr.Points[0].T
		}
	}
	dayStart := minT - minT%86400
	valStart := dayStart + valAfterHours*3600
	testStart := dayStart + testAfterHours*3600

	splits := map[string]*splitWriter{
		"train": newSplit("train"),
		"val":   newSplit("val"),
		"test":  newSplit("test"),
	}

	for _, tr := range tracks {
		pts := tr.Points
		var lastAnchor int64
		for i, pt := range pts {
			if pt.SpeedKn < minSpeedKn {
				continue
			}

			// Which split is this question in, and how far apart are questions?
			split, spacing, limit := "test", int64(600), int64(1<<62)
			switch {
			case pt.T < valStart:
				split, spacing, limit = "train", 300, valStart
			case pt.T < testStart:
				split, spacing, limit = "val", 300, testStart
			}
			if pt.T-lastAnchor < spacing {
				continue
			}
			lastAnchor = pt.T

			for _, h := range horizons {
				target := pt.T + h
				j := sort.Search(len(pts), func(k int) bool { return pts[k].T >= target-truthSlack })
				if j == len(pts) || pts[j].T > target+truthSlack {
					continue
				}
				truth := pts[j]
				if truth.T >= limit {
					continue // answer falls in the next split: would leak the future
				}

				in, ok := features.Inputs(pts[:i+1], truth.T-pt.T)
				if !ok {
					continue
				}
				tdx, tdy := features.LocalKm(pt.Lat, pt.Lon, truth.Lat, truth.Lon)
				splits[split].write(append(in, float32(tdx), float32(tdy)))
			}
		}
	}

	rows := map[string]int{}
	for name, s := range splits {
		s.close()
		rows[name] = s.rows
	}

	meta := map[string]any{
		"columns":       features.ColumnNames(),
		"num_inputs":    features.NumInputs,
		"hist_len":      features.HistLen,
		"step_features": features.StepFeatures,
		"rows":          rows,
		"source":        os.Args[1],
	}
	mf, err := os.Create(filepath.Join(outDir, "meta.json"))
	if err != nil {
		log.Fatal(err)
	}
	enc := json.NewEncoder(mf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(meta); err != nil {
		log.Fatal(err)
	}
	mf.Close()

	fmt.Printf("rows: train %d, val %d, test %d → %s\n", rows["train"], rows["val"], rows["test"], outDir)
}
