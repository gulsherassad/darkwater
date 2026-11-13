package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"sort"

	"github.com/gulsherassad/darkwater/internal/dma"
	"github.com/gulsherassad/darkwater/internal/geo"
	"github.com/gulsherassad/darkwater/internal/grid"
	"github.com/gulsherassad/darkwater/internal/predict"
	"github.com/gulsherassad/darkwater/internal/track"
)

var horizons = []int64{5 * 60, 15 * 60, 30 * 60, 60 * 60} // seconds ahead

const (
	testAfterHours = 18  // test set = everything after 18:00 of the day
	anchorEvery    = 600 // at most one test question per ship every 10 min
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
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/trajeval <dma.csv>")
		os.Exit(2)
	}

	// 1) Build per-ship tracks (one point per minute) from the DMA file
	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	rd, err := dma.NewReader(f)
	if err != nil {
		log.Fatal(err)
	}
	b := track.NewBuilder()
	for {
		p, reason, err := rd.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		if reason != "" || p.Validate() != "" || !grid.InDenmark(p) {
			continue
		}
		b.Add(p)
	}
	tracks := b.Tracks()

	var minT int64 = -1
	points := 0
	for _, tr := range tracks {
		points += len(tr.Points)
		if minT < 0 || tr.Points[0].T < minT {
			minT = tr.Points[0].T
		}
	}
	dayStart := minT - minT%86400
	testStart := dayStart + testAfterHours*3600
	fmt.Printf("tracks: %d ships, %d points (1/min)\n", len(tracks), points)

	// 2) The exam: same questions for every predictor
	predictors := []predict.Predictor{predict.StayPut{}, predict.DeadReckoning{}}
	errs := map[string]map[int64][]float64{} // predictor -> horizon -> errors in km
	for _, p := range predictors {
		errs[p.Name()] = map[int64][]float64{}
	}

	for _, tr := range tracks {
		pts := tr.Points
		var lastAnchor int64
		for i, pt := range pts {
			if pt.T < testStart || pt.T-lastAnchor < anchorEvery || pt.SpeedKn < minSpeedKn {
				continue
			}
			lastAnchor = pt.T
			history := pts[:i+1] // everything up to now; the future stays hidden

			for _, h := range horizons {
				target := pt.T + h
				// First point at or after target - slack
				j := sort.Search(len(pts), func(k int) bool { return pts[k].T >= target-truthSlack })
				if j == len(pts) || pts[j].T > target+truthSlack {
					continue // no ground truth near the target time
				}
				truth := pts[j]
				for _, p := range predictors {
					lat, lon := p.Predict(history, truth.T-pt.T)
					errs[p.Name()][h] = append(errs[p.Name()][h], geo.HaversineKm(lat, lon, truth.Lat, truth.Lon))
				}
			}
		}
	}

	// 3) Results
	fmt.Printf("\ntest set: after %02d:00 UTC, moving ships (>= %d kn), one question per ship per 10 min\n\n",
		testAfterHours, minSpeedKn)
	fmt.Printf("%-8s %6s", "horizon", "n")
	for _, p := range predictors {
		fmt.Printf("   %-24s", p.Name()+" (median / p90)")
	}
	fmt.Println()
	for _, h := range horizons {
		n := len(errs[predictors[0].Name()][h])
		fmt.Printf("%-8s %6d", fmt.Sprintf("%d min", h/60), n)
		for _, p := range predictors {
			e := errs[p.Name()][h]
			fmt.Printf("   %6.2f km / %6.2f km     ", percentile(e, 0.5), percentile(e, 0.9))
		}
		fmt.Println()
	}
}
