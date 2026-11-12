package grid

import (
	h3 "github.com/uber/h3-go/v4"

	"github.com/gulsherassad/darkwater/internal/ais"
)

// Res is the hexagon size: resolution 6 ≈ 36 km² per cell (~3 km per side).
const Res = 6

// CellOf returns the hexagon a position falls in.
func CellOf(p ais.Position) h3.Cell {
	return h3.LatLngToCell(h3.NewLatLng(p.Lat, p.Lon), Res)
}

// InDenmark reports whether p is inside our study area
// (same box as the live feed subscription).
func InDenmark(p ais.Position) bool {
	return p.Lat >= 54 && p.Lat <= 58.5 && p.Lon >= 7 && p.Lon <= 16
}
