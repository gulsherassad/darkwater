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
	modelPath := flag.String("model", "models/traj_gru.onnx", "dense GRU (\"\" to skip)")
	sparsePath := flag.String("sparse-model", "", "sparse GRU, e.g. models/traj_gru_sparse.onnx (optional)")
	libPath := flag.String("ortlib", "/opt/homebrew/lib/libonnxruntime.dylib", "ONNX Runtime library")
	thinPath := flag.String("thin", "", "live gap list (data/ml/live-gaps.csv): ask questions on thinned tracks")
	seed := flag.Int64("seed", 1, "thinning seed")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/trajeval [-model path] [-sparse-model path] [-thin gaps.csv -seed N] [-ortlib path] <dma.csv>")
		os.Exit(2)
	}

	// Models: each picks its feature recipe from its input width
	var gru, sparse *model.GRU
	var gruModels []*model.GRU
	for _, m := range []struct {
		path string
		dst  **model.GRU
	}{{*modelPath, &gru}, {*sparsePath, &sparse}} {
		if m.path == "" {
			continue
		}
		g, err := model.LoadGRU(m.path, *libPath)
		if err != nil {
			log.Fatal(err)
		}
		defer g.Close()
		fmt.Printf("loaded %s (%s features)\n", m.path, g.Spec.Version)
		*m.dst = g
		gruModels = append(gruModels, g)
	}
	if gru != nil && sparse != nil && gru.Spec.Version == sparse.Spec.Version {
		log.Fatal("-model and -sparse-model use the same feature recipe")
	}

	var gaps []int64
	if *thinPath != "" {
		var err error
		if gaps, err = track.LoadGaps(*thinPath); err != nil {
			log.Fatal(err)
		}
	}

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
	if gaps != nil {
		fmt.Printf("histories thinned to live-feed gaps (%s, %d gaps, seed %d); truth from the full track\n",
			*thinPath, len(gaps), *seed)
	}

	predictors := []predict.Predictor{predict.StayPut{}, predict.DeadReckoning{}}
	hybrid := predict.Hybrid{Short: predict.DeadReckoning{}, SwitchSec: 600}
	// Assign only non-nil models: a nil *GRU in an interface isn't a nil interface
	if gru != nil {
		predictors = append(predictors, gru)
		hybrid.Long = gru
	}
	if sparse != nil {
		predictors = append(predictors, sparse)
		hybrid.Sparse = sparse
	}
	predictors = append(predictors, hybrid)

	errs := map[string]map[int64][]float64{}
	for _, p := range predictors {
		errs[p.Name()] = map[int64][]float64{}
	}

	// Per-path breakdown: which branch the hybrid took, and DR vs sparse GRU
	// on only the questions the sparse GRU answered
	paths := map[int64]map[string]int{}
	type pair struct{ dr, gru []float64 }
	sparseOnly := map[int64]*pair{}
	for _, h := range horizons {
		paths[h], sparseOnly[h] = map[string]int{}, &pair{}
	}

	// The exam: identical questions for every predictor
	questions := 0
	for _, tr := range tracks {
		pts := tr.Points // full track: the truth
		hist := pts      // what the predictors see
		if gaps != nil {
			hist = track.Thin(pts, gaps, track.ThinRNG(*seed, tr.MMSI))
		}
		var lastAnchor int64
		for i, pt := range hist {
			if pt.T < testStart || pt.T-lastAnchor < anchorEvery || pt.SpeedKn < minSpeedKn {
				continue
			}
			lastAnchor = pt.T
			history := hist[:i+1]

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
				_, path := hybrid.Pick(history, truth.T-pt.T)
				paths[h][path]++
				if path == predict.PathSparse { // this question's errors were just appended last
					dr, gru := errs["dead-reckoning"][h], errs[sparse.Name()][h]
					sparseOnly[h].dr = append(sparseOnly[h].dr, dr[len(dr)-1])
					sparseOnly[h].gru = append(sparseOnly[h].gru, gru[len(gru)-1])
				}
			}
		}
	}

	// Results: one block per horizon, one row per predictor
	fmt.Printf("\ntest set: after %02d:00 UTC, moving ships (>= %d kn), one question per ship per 10 min\n",
		testAfterHours, minSpeedKn)
	// Fallbacks count only direct calls: the hybrid checks Usable first and skips models that can't answer
	for _, g := range gruModels {
		fmt.Printf("%s fallbacks to dead reckoning: %d calls, model errors: %d\n", g.Name(), g.Fallbacks, g.Errors)
	}
	fmt.Println()

	pct := func(v, dr float64) float64 {
		if dr == 0 {
			return 0
		}
		return 100 * (v - dr) / dr
	}
	fmt.Printf("%-8s %6s   %-15s %8s %8s %10s %10s\n", "horizon", "n", "predictor", "median", "p90", "p50 vs DR", "p90 vs DR")
	for _, h := range horizons {
		drP50 := percentile(errs["dead-reckoning"][h], 0.5)
		drP90 := percentile(errs["dead-reckoning"][h], 0.9)
		for k, p := range predictors {
			e := errs[p.Name()][h]
			label, n := "", ""
			if k == 0 {
				label, n = fmt.Sprintf("%d min", h/60), fmt.Sprint(len(e))
			}
			p50, p90 := percentile(e, 0.5), percentile(e, 0.9)
			fmt.Printf("%-8s %6s   %-15s %6.2f km %6.2f km %+9.1f%% %+9.1f%%\n",
				label, n, p.Name(), p50, p90, pct(p50, drP50), pct(p90, drP90))
		}
		fmt.Println()
	}
	fmt.Printf("%d questions total\n", questions)

	// Per-path breakdown of the hybrid
	fmt.Printf("\nhybrid: share of questions per path\n")
	fmt.Printf("%-8s %6s %12s %10s %11s %9s\n", "horizon", "n", "DR (<10min)", "dense GRU", "sparse GRU", "fallback")
	for _, h := range horizons {
		c := paths[h]
		n := c[predict.PathShort] + c[predict.PathLong] + c[predict.PathSparse] + c[predict.PathFallback]
		share := func(path string) float64 { return 100 * float64(c[path]) / float64(max(n, 1)) }
		fmt.Printf("%-8s %6d %11.1f%% %9.1f%% %10.1f%% %8.1f%%\n", fmt.Sprintf("%d min", h/60), n,
			share(predict.PathShort), share(predict.PathLong), share(predict.PathSparse), share(predict.PathFallback))
	}
	if sparse != nil {
		fmt.Printf("\nonly questions answered by the sparse GRU\n")
		fmt.Printf("%-8s %6s %18s %18s %10s\n", "horizon", "n", "DR p50 / p90", "GRU p50 / p90", "p90 vs DR")
		for _, h := range horizons {
			s := sparseOnly[h]
			if len(s.gru) == 0 {
				continue // e.g. 5 min: always dead reckoning
			}
			drP90, gruP90 := percentile(s.dr, 0.9), percentile(s.gru, 0.9)
			fmt.Printf("%-8s %6d %6.2f / %5.2f km %6.2f / %5.2f km %+9.1f%%\n", fmt.Sprintf("%d min", h/60), len(s.gru),
				percentile(s.dr, 0.5), drP90, percentile(s.gru, 0.5), gruP90, pct(gruP90, drP90))
		}
	}
}
