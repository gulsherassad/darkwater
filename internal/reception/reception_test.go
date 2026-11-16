package reception

import (
	"math"
	"testing"

	h3 "github.com/uber/h3-go/v4"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/grid"
)

func cell(lat, lon float64) h3.Cell {
	return grid.CellOf(ais.Position{Lat: lat, Lon: lon})
}

func TestSmoothing(t *testing.T) {
	good, poor, unseen := cell(55.7, 12.6), cell(57.0, 11.0), cell(56.5, 8.0)
	m := Fit(map[h3.Cell]Counts{
		good: {Present: 1000, Heard: 500}, // 50%
		poor: {Present: 1000, Heard: 100}, // 10%
	}, 20)

	if math.Abs(m.Global-0.3) > 1e-9 {
		t.Fatalf("global should be 0.3, got %.3f", m.Global)
	}
	if math.Abs(m.Prob(good)-0.5) > 0.01 {
		t.Fatalf("lots of data: should stay near its own 50%%, got %.3f", m.Prob(good))
	}
	if m.Prob(unseen) != m.Global {
		t.Fatalf("no data: should equal the global rate, got %.3f", m.Prob(unseen))
	}
}

func TestSilenceLogProb(t *testing.T) {
	c := cell(55.7, 12.6)
	m := Fit(map[h3.Cell]Counts{c: {Present: 1000, Heard: 500}}, 20)
	p := m.Prob(c)

	got := math.Exp(m.SilenceLogProb([]h3.Cell{c, c, c}))
	want := math.Pow(1-p, 3)
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("3 silent minutes: want %.6f, got %.6f", want, got)
	}
}
