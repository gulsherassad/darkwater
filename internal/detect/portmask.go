package detect

import (
	"math"

	"github.com/gulsherassad/darkwater/internal/ais"
)

const (
	navMoored    = 5   // AIS navigational status "moored"
	stationaryKn = 0.5 // slower than this = sitting still
)

// PortMask marks ~1 km squares where ships regularly sit still: ports,
// marinas, fishing harbours, anchorages. Learned from behaviour, not the
// status field, because many ships never set "moored".
// Use: Add every position, call Finish once, then ask Masked.
type PortMask struct {
	still map[[2]int32]map[int64]bool // square -> ships seen sitting still there
	cells map[[2]int32]bool           // final masked squares
}

func NewPortMask() *PortMask {
	return &PortMask{still: map[[2]int32]map[int64]bool{}}
}

// 0.01° squares: ~1.1 km north-south, ~0.6 km east-west at Danish latitudes
func squareOf(lat, lon float64) [2]int32 {
	return [2]int32{int32(math.Floor(lat * 100)), int32(math.Floor(lon * 100))}
}

func (m *PortMask) Add(p ais.Position) {
	stationary := p.SpeedKn >= 0 && p.SpeedKn < stationaryKn // -1 = unknown speed
	if p.NavStatus != navMoored && !stationary {
		return
	}
	sq := squareOf(p.Lat, p.Lon)
	if m.still[sq] == nil {
		m.still[sq] = map[int64]bool{}
	}
	m.still[sq][p.MMSI] = true
}

// Finish masks every square where at least minShips different ships sat still,
// plus its 8 neighbours so harbour edges are covered. Returns the number of port squares.
func (m *PortMask) Finish(minShips int) int {
	m.cells = map[[2]int32]bool{}
	ports := 0
	for sq, ships := range m.still {
		if len(ships) < minShips {
			continue // one or two ships stopping at sea doesn't make a port
		}
		ports++
		for dy := int32(-1); dy <= 1; dy++ {
			for dx := int32(-1); dx <= 1; dx++ {
				m.cells[[2]int32{sq[0] + dy, sq[1] + dx}] = true
			}
		}
	}
	m.still = nil
	return ports
}

func (m *PortMask) Masked(lat, lon float64) bool {
	return m.cells[squareOf(lat, lon)]
}
