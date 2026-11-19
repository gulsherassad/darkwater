package detect

import (
	"testing"

	"github.com/gulsherassad/darkwater/internal/ais"
)

func fakeInfo(types map[int64]int) func(int64) (ais.ShipInfo, bool) {
	return func(m int64) (ais.ShipInfo, bool) {
		t, ok := types[m]
		return ais.ShipInfo{MMSI: m, Type: t}, ok
	}
}

func TestEncounterTypeFilter(t *testing.T) {
	const a, b = 219000010, 219000011 // the two ships simulate() uses
	cases := []struct {
		name  string
		types map[int64]int
		want  int // encounter_start events expected
	}{
		{"two rescue boats", map[int64]int{a: 51, b: 51}, 0},
		{"pilot alongside tanker", map[int64]int{a: 50, b: 80}, 0},
		{"two fishing boats", map[int64]int{a: 30, b: 30}, 0},
		{"tanker and cargo ship", map[int64]int{a: 80, b: 70}, 1},
		{"tanker and fishing boat (transshipment)", map[int64]int{a: 80, b: 30}, 1},
		{"tanker and unknown", map[int64]int{a: 80}, 1},
		{"both unknown", map[int64]int{}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := NewEncounterDetector()
			d.Info = fakeInfo(c.types)
			got := encEvents(d)
			simulate(d, 0, 60, 55.5004, 0.5)
			starts := 0
			for _, e := range *got {
				if e.Kind == EncounterStart {
					starts++
				}
			}
			if starts != c.want {
				t.Fatalf("want %d encounters, got %d: %+v", c.want, starts, *got)
			}
		})
	}
}
