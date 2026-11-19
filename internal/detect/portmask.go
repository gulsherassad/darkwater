package detect

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"

	"github.com/gulsherassad/darkwater/internal/ais"
)

const (
	navMoored    = 5   // AIS navigational status "moored"
	stationaryKn = 0.5 // slower than this = sitting still
)

// PortMask marks ~1 km squares where ships regularly sit still: ports,
// marinas, fishing harbours, anchorages. Learned from behaviour, not the
// status field, because many ships never set "moored".
// Use: Add every position, call Finish once, then ask Masked (or Save it).
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

// Save writes every masked square as "lat×100,lon×100", sorted so the file diffs cleanly.
func (m *PortMask) Save(path string) error {
	keys := make([][2]int32, 0, len(m.cells))
	for sq := range m.cells {
		keys = append(keys, sq)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	fmt.Fprintln(w, "lat100,lon100")
	for _, sq := range keys {
		fmt.Fprintf(w, "%d,%d\n", sq[0], sq[1])
	}
	return w.Flush()
}

// LoadPortMask reads a mask written by Save.
func LoadPortMask(path string) (*PortMask, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	m := &PortMask{cells: map[[2]int32]bool{}}
	for i, row := range rows {
		if i == 0 {
			continue // header
		}
		lat, err1 := strconv.ParseInt(row[0], 10, 32)
		lon, err2 := strconv.ParseInt(row[1], 10, 32)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("%s line %d: bad square %v", path, i+1, row)
		}
		m.cells[[2]int32{int32(lat), int32(lon)}] = true
	}
	return m, nil
}
