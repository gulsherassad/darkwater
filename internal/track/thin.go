package track

import (
	"bufio"
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
)

// Thin makes a dense track look like the live feed: start at the first fix,
// then repeatedly draw a gap from gaps (seconds, e.g. measured by cmd/livegaps)
// and keep the first fix at or after (last kept time + gap).
func Thin(pts []Point, gaps []int64, rng *rand.Rand) []Point {
	if len(pts) == 0 || len(gaps) == 0 {
		return pts
	}
	out := []Point{pts[0]}
	i := 0
	for {
		gap := max(gaps[rng.IntN(len(gaps))], 1)
		next := out[len(out)-1].T + gap
		for i < len(pts) && pts[i].T < next {
			i++
		}
		if i == len(pts) {
			return out
		}
		out = append(out, pts[i])
	}
}

// ThinRNG gives every (seed, ship) pair its own random sequence, so a ship is
// thinned the same way whatever order the tracks are processed in.
func ThinRNG(seed, mmsi int64) *rand.Rand {
	return rand.New(rand.NewPCG(uint64(seed), uint64(mmsi)))
}

// LoadGaps reads a gap list written by cmd/livegaps: a header line, then one
// gap in seconds per line.
func LoadGaps(path string) ([]int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var gaps []int64
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		s := strings.TrimSpace(sc.Text())
		if line == 1 || s == "" {
			continue // header
		}
		g, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		gaps = append(gaps, g)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(gaps) == 0 {
		return nil, fmt.Errorf("%s: no gaps", path)
	}
	return gaps, nil
}
