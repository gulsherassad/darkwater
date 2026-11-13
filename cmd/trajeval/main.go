package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/gulsherassad/darkwater/internal/geo"
	"github.com/gulsherassad/darkwater/internal/model"
	"github.com/gulsherassad/darkwater/internal/predict"
	"github.com/gulsherassad/darkwater/internal/track"
)

var horizons = []int64{5 * 60, 15 * 60, 30 * 60, 60 * 60}

const (
	testAfterHours = 18  // test set = everything after 18:00 of the day
	anchorEvery    = 600 // at most one question per ship every 10 min
	minSpeedKn     = 3   // only test ships that are actually moving
	truthSlack     = 60  // ground truth must be within ±60 s of the target time
)

func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[int(p*float64(len(s)-1))]
}

func main() {
	modelPath := flag.String("model", "models/traj_gru.onnx", "trained ONNX model")
	libPath := flag.String("ortlib", "/opt/homebrew/lib/libonnxruntime.dylib", "ONNX Runtime library")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/trajeval [-model path] [-ortlib path] <dma.csv>")
		os.Exit(2)
	}

	gru, err := model.LoadGRU(*modelPath, *libPath)
	if err != nil {
		log.Fatal(err)
	}
	defer gru.Close()

	tracks, err := track.LoadDMA(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	var minT int64 = -1
	points := 0
	for _, tr := range tracks {
		points += len(tr.Points)
		if minT < 0 || tr.Points[0].T < minT {
			minT = tr.Points[0].T
		}
	}
	testStart := minT - minT%86400 + testAfterHours*3600
	fmt.Printf("tracks: %d ships, %d points (1/min)\n", len(tracks), points)

	predictors := []predict.Predictor{
		predict.StayPut{},
		predict.DeadReckoning{},
		gru,
		predict.Hybrid{Short: predict.DeadReckoning{}, Long: gru, SwitchSec: 600},
	}
	errs := map[string]map[int64][]float64{}
	for _, p := range predictors {
		errs[p.Name()] = map[int64][]float64{}
	}

	// The exam: identical questions for every predictor
	questions := 0
	for _, tr := range tracks {
		pts := tr.Points
		var lastAnchor int64
		for i, pt := range pts {
			if pt.T < testStart || pt.T-lastAnchor < anchorEvery || pt.SpeedKn < minSpeedKn {
				continue
			}
			lastAnchor = pt.T
			history := pts[:i+1]

			for _, h := range horizons {
				target := pt.T + h
				j := sort.Search(len(pts), func(k int) bool { return pts[k].T >= target-truthSlack })
				if j == len(pts) || pts[j].T > target+truthSlack {
					continue
				}
				truth := pts[j]
				questions++
				for _, p := range predictors {
					lat, lon := p.Predict(history, truth.T-pt.T)
					errs[p.Name()][h] = append(errs[p.Name()][h], geo.HaversineKm(lat, lon, truth.Lat, truth.Lon))
				}
			}
		}
	}

	// Results: one block per horizon, one row per predictor
	fmt.Printf("\ntest set: after %02d:00 UTC, moving ships (>= %d kn), one question per ship per 10 min\n",
		testAfterHours, minSpeedKn)
	// The GRU is called twice per question (once directly, once inside hybrid at >= 10 min)
	fmt.Printf("gru fallbacks to dead reckoning: %d calls, model errors: %d\n\n", gru.Fallbacks, gru.Errors)

	fmt.Printf("%-8s %6s   %-15s %8s %8s %10s\n", "horizon", "n", "predictor", "median", "p90", "p90 vs DR")
	for _, h := range horizons {
		drP90 := percentile(errs["dead-reckoning"][h], 0.9)
		for k, p := range predictors {
			e := errs[p.Name()][h]
			label, n := "", ""
			if k == 0 {
				label, n = fmt.Sprintf("%d min", h/60), fmt.Sprint(len(e))
			}
			p90 := percentile(e, 0.9)
			fmt.Printf("%-8s %6s   %-15s %6.2f km %6.2f km %+9.1f%%\n",
				label, n, p.Name(), percentile(e, 0.5), p90, 100*(p90-drP90)/drP90)
		}
		fmt.Println()
	}
	fmt.Printf("%d questions total\n", questions)
}
