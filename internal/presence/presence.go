// Package presence turns a DMA day into "which ship was where, minute by
// minute": the ground truth used to fit the reception model (cmd/reception)
// and to evaluate the gap detector (cmd/inject).
package presence

import (
	"encoding/binary"
	"hash/fnv"
	"io"
	"os"

	"github.com/gulsherassad/darkwater/internal/dma"
	"github.com/gulsherassad/darkwater/internal/grid"
)

// MaxInterpMin: DMA gaps up to this long are filled in (the ship was obviously still there).
const MaxInterpMin = 15

// Key is one ship in one minute (Unix seconds / 60).
type Key struct {
	MMSI, Minute int64
}

// InFitSet splits ships into two random halves. MMSIs aren't random
// (big ships often end in zeros), so we hash them first.
func InFitSet(mmsi int64) bool {
	h := fnv.New32a()
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(mmsi))
	h.Write(b[:])
	return h.Sum32()%2 == 0
}

// Fix is where a ship was in one minute.
type Fix struct {
	Minute   int64
	Lat, Lon float64
	SpeedKn  float64 // -1 = unknown
}

// LoadDMA reads one fix per ship per minute (the first in that minute), for
// valid positions inside the study area, keeping minutes in [from, to].
func LoadDMA(path string, from, to int64) (map[int64][]Fix, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rd, err := dma.NewReader(f)
	if err != nil {
		return nil, err
	}
	fixes := map[int64][]Fix{}
	for {
		p, reason, err := rd.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if reason != "" || p.Validate() != "" || !grid.InDenmark(p) {
			continue
		}
		m := p.Time.Unix() / 60
		if m < from || m > to {
			continue // far from any minute we can use
		}
		fs := fixes[p.MMSI]
		if n := len(fs); n > 0 && fs[n-1].Minute >= m {
			continue // already have a fix for this minute
		}
		fixes[p.MMSI] = append(fs, Fix{m, p.Lat, p.Lon, p.SpeedKn})
	}
	return fixes, nil
}

// Walk calls fn for every minute a ship was present: DMA's own fixes
// (observed = true), plus minutes inside DMA gaps of at most MaxInterpMin,
// with position and speed interpolated in a straight line.
func Walk(fixes map[int64][]Fix, fn func(mmsi int64, f Fix, observed bool)) {
	for mmsi, fs := range fixes {
		for i, a := range fs {
			fn(mmsi, a, true)
			if i+1 == len(fs) {
				break
			}
			b := fs[i+1]
			span := b.Minute - a.Minute
			if span <= 1 || span > MaxInterpMin {
				continue // consecutive minutes, or a gap too long to trust
			}
			for m := a.Minute + 1; m < b.Minute; m++ {
				t := float64(m-a.Minute) / float64(span)
				speed := -1.0
				if a.SpeedKn >= 0 && b.SpeedKn >= 0 {
					speed = a.SpeedKn + t*(b.SpeedKn-a.SpeedKn)
				}
				fn(mmsi, Fix{m, a.Lat + t*(b.Lat-a.Lat), a.Lon + t*(b.Lon-a.Lon), speed}, false)
			}
		}
	}
}
