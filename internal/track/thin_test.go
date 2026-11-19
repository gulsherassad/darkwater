package track

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// One fix per minute for an hour
func minuteTrack() []Point {
	var pts []Point
	for i := int64(0); i <= 60; i++ {
		pts = append(pts, Point{T: 1000 + i*60, Lat: 55, Lon: 11})
	}
	return pts
}

func TestThinFixedGap(t *testing.T) {
	// Every gap is 150 s: keep 0, then first fix at/after 150 s (180), then 330 → 360, ...
	got := Thin(minuteTrack(), []int64{150}, ThinRNG(1, 1))
	var want []int64
	for s := int64(0); s <= 3600; s += 180 {
		want = append(want, 1000+s)
	}
	var times []int64
	for _, p := range got {
		times = append(times, p.T)
	}
	if !reflect.DeepEqual(times, want) {
		t.Fatalf("want %v\ngot  %v", want, times)
	}
}

func TestThinStartsAtFirstFixAndRespectsGaps(t *testing.T) {
	pts := minuteTrack()
	gaps := []int64{30, 200, 400}
	got := Thin(pts, gaps, ThinRNG(7, 219000001))
	if got[0] != pts[0] {
		t.Fatal("thinned track must start at the first fix")
	}
	for i := 1; i < len(got); i++ {
		if d := got[i].T - got[i-1].T; d < 30 {
			t.Fatalf("gap %d s is shorter than any drawn gap", d)
		}
	}
	if len(got) >= len(pts) {
		t.Fatal("thinning should drop fixes")
	}
}

func TestThinReproducible(t *testing.T) {
	gaps := []int64{60, 120, 180, 240, 300}
	a := Thin(minuteTrack(), gaps, ThinRNG(42, 5))
	b := Thin(minuteTrack(), gaps, ThinRNG(42, 5))
	c := Thin(minuteTrack(), gaps, ThinRNG(43, 5))
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same seed must give the same thinning")
	}
	if reflect.DeepEqual(a, c) {
		t.Fatal("different seeds should give different thinning")
	}
}

func TestThinEdgeCases(t *testing.T) {
	if got := Thin(nil, []int64{60}, ThinRNG(1, 1)); len(got) != 0 {
		t.Fatal("empty track stays empty")
	}
	pts := minuteTrack()
	if got := Thin(pts, []int64{0}, ThinRNG(1, 1)); len(got) != len(pts) {
		t.Fatal("zero gaps should keep every fix (gap is at least 1 s)")
	}
}

func TestLoadGaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gaps.csv")
	if err := os.WriteFile(path, []byte("gap_sec\n180\n240\n\n61\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gaps, err := LoadGaps(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gaps, []int64{180, 240, 61}) {
		t.Fatalf("got %v", gaps)
	}
}
