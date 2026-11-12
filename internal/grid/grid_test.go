package grid

import (
	"testing"

	"github.com/gulsherassad/darkwater/internal/ais"
)

func TestCellOf(t *testing.T) {
	copenhagen := ais.Position{Lat: 55.6761, Lon: 12.5683}
	c := CellOf(copenhagen)
	if !c.IsValid() {
		t.Fatalf("invalid cell %v", c)
	}
	if c.Resolution() != Res {
		t.Fatalf("want resolution %d, got %d", Res, c.Resolution())
	}

	// Gothenburg is ~250 km away: must be a different cell
	gothenburg := ais.Position{Lat: 57.7089, Lon: 11.9746}
	if CellOf(gothenburg) == c {
		t.Fatal("Copenhagen and Gothenburg should not share a cell")
	}
}

func TestInDenmark(t *testing.T) {
	if !InDenmark(ais.Position{Lat: 55.67, Lon: 12.56}) {
		t.Fatal("Copenhagen should be inside")
	}
	if InDenmark(ais.Position{Lat: 59.91, Lon: 10.75}) {
		t.Fatal("Oslo should be outside")
	}
}
