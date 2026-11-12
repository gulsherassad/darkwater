package api

import (
	"testing"

	"github.com/gulsherassad/darkwater/internal/ais"
)

func TestFilterByView(t *testing.T) {
	ships := []ais.Position{
		{MMSI: 1, Lat: 55.7, Lon: 12.6}, // Copenhagen: inside
		{MMSI: 2, Lat: 57.7, Lon: 11.9}, // Gothenburg: outside
	}
	view := &bbox{West: 12.0, South: 55.0, East: 13.0, North: 56.0}

	got := filter(ships, view)
	if len(got) != 1 || got[0].MMSI != 1 {
		t.Fatalf("want only Copenhagen ship, got %+v", got)
	}

	if all := filter(ships, nil); len(all) != 2 {
		t.Fatalf("nil view should return everything, got %d", len(all))
	}
}

func TestViewportValidation(t *testing.T) {
	good := viewportMsg{Type: "viewport", West: 7, South: 54, East: 16, North: 58.5}
	if !good.valid() {
		t.Fatal("Denmark viewport should be valid")
	}

	bad := []viewportMsg{
		{Type: "viewport", West: 7, South: 58, East: 16, North: 54},  // south above north
		{Type: "viewport", West: 7, South: 54, East: 16, North: 500}, // impossible latitude
		{Type: "hello", West: 7, South: 54, East: 16, North: 58.5},   // wrong type
	}
	for _, m := range bad {
		if m.valid() {
			t.Fatalf("should be invalid: %+v", m)
		}
	}
}
