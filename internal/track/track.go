package track

import (
	"sort"

	"github.com/gulsherassad/darkwater/internal/ais"
)

// Point is a compact track point (no name or status, to save memory).
type Point struct {
	T         int64 // Unix seconds
	Lat, Lon  float64
	SpeedKn   float32 // -1 = unknown
	CourseDeg float32 // -1 = unknown
}

type Track struct {
	MMSI   int64
	Points []Point // oldest first
}

// Builder collects positions into per-ship tracks, at most one point per minute.
type Builder struct {
	tracks map[int64]*Track
}

func NewBuilder() *Builder {
	return &Builder{tracks: map[int64]*Track{}}
}

// Add appends p to its ship's track, unless that ship already has
// a point in the same minute (or a newer one).
func (b *Builder) Add(p ais.Position) {
	t := p.Time.Unix()
	tr := b.tracks[p.MMSI]
	if tr == nil {
		tr = &Track{MMSI: p.MMSI}
		b.tracks[p.MMSI] = tr
	}
	if n := len(tr.Points); n > 0 && t/60 <= tr.Points[n-1].T/60 {
		return
	}
	tr.Points = append(tr.Points, Point{
		T:         t,
		Lat:       p.Lat,
		Lon:       p.Lon,
		SpeedKn:   float32(p.SpeedKn),
		CourseDeg: float32(p.CourseDeg),
	})
}

// Tracks returns all tracks, sorted by MMSI so results are reproducible.
func (b *Builder) Tracks() []*Track {
	out := make([]*Track, 0, len(b.tracks))
	for _, tr := range b.tracks {
		out = append(out, tr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MMSI < out[j].MMSI })
	return out
}
