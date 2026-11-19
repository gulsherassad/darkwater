package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/gulsherassad/darkwater/internal/features"
	"github.com/gulsherassad/darkwater/internal/track"
)

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

func newSplit(outDir, name string) *splitWriter {
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
	featureFlag := flag.String("features", "dense", "input recipe: dense (v1, DMA-rate history) or sparse (v2, live-feed gaps)")
	outFlag := flag.String("out", "", "output dir (default data/ml for dense, data/ml/sparse for sparse)")
	thinPath := flag.String("thin", "", "live gap list (data/ml/live-gaps.csv): thin every track to live-feed density first")
	seed := flag.Int64("seed", 1, "thinning seed (pass k uses seed+k)")
	passes := flag.Int("passes", 1, "thinning passes per track, each with its own seed")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/trajexport [-features dense|sparse] [-thin gaps.csv -seed N -passes N] [-out dir] <dma.csv>")
		os.Exit(2)
	}
	src := flag.Arg(0)

	var spec features.Spec
	outDir := *outFlag
	switch *featureFlag {
	case "dense":
		spec = features.Dense
		if outDir == "" {
			outDir = "data/ml"
		}
	case "sparse":
		spec = features.Sparse
		if outDir == "" {
			outDir = "data/ml/sparse"
		}
	default:
		log.Fatalf("unknown -features %q (want dense or sparse)", *featureFlag)
	}

	var gaps []int64
	if *thinPath != "" {
		var err error
		if gaps, err = track.LoadGaps(*thinPath); err != nil {
			log.Fatal(err)
		}
	} else if *passes != 1 {
		log.Fatal("-passes needs -thin (without thinning every pass would be identical)")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatal(err)
	}

	tracks, err := track.LoadDMA(src)
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
		"train": newSplit(outDir, "train"),
		"val":   newSplit(outDir, "val"),
		"test":  newSplit(outDir, "test"),
	}

	for pass := 0; pass < *passes; pass++ {
		for _, tr := range tracks {
			pts := tr.Points // dense track: ground truth always comes from here
			hist := pts      // what the model sees: thinned when -thin is set
			if gaps != nil {
				hist = track.Thin(pts, gaps, track.ThinRNG(*seed+int64(pass), tr.MMSI))
			}
			var lastAnchor int64
			for i, pt := range hist {
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

					in, ok := spec.Inputs(hist[:i+1], truth.T-pt.T)
					if !ok {
						continue
					}
					tdx, tdy := features.LocalKm(pt.Lat, pt.Lon, truth.Lat, truth.Lon)
					splits[split].write(append(in, float32(tdx), float32(tdy)))
				}
			}
		}
	}

	rows := map[string]int{}
	for name, s := range splits {
		s.close()
		rows[name] = s.rows
	}

	meta := map[string]any{
		"feature_version": spec.Version,
		"columns":         spec.ColumnNames(),
		"num_inputs":      spec.NumInputs(),
		"hist_len":        spec.HistLen,
		"step_features":   spec.StepFeatures(),
		"max_span_sec":    spec.MaxSpanSec,
		"rows":            rows,
		"source":          src,
		"thin":            *thinPath, // "" = not thinned
		"seed":            *seed,
		"passes":          *passes,
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

	fmt.Printf("%s: rows train %d, val %d, test %d → %s\n", spec.Version, rows["train"], rows["val"], rows["test"], outDir)
}
