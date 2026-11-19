package detect

import "testing"

func TestStationaryShipsMakeAPort(t *testing.T) {
	mask := NewPortMask()

	// Three fishing boats berthed in a harbour, all wrongly reporting "under way"
	for i := int64(0); i < 3; i++ {
		p := pos(219000200+i, 0, 57.32, 11.13)
		p.NavStatus = 0
		p.SpeedKn = 0.1
		mask.Add(p)
	}
	// Three ships passing through another square at speed
	for i := int64(0); i < 3; i++ {
		p := pos(219000210+i, 0, 56.0, 8.0)
		p.SpeedKn = 10
		mask.Add(p)
	}
	// Unknown speed shouldn't count as sitting still
	for i := int64(0); i < 3; i++ {
		p := pos(219000220+i, 0, 55.0, 9.0)
		p.SpeedKn = -1
		mask.Add(p)
	}

	if n := mask.Finish(3); n != 1 {
		t.Fatalf("want 1 port square, got %d", n)
	}
	if !mask.Masked(57.32, 11.13) {
		t.Fatal("harbour with berthed boats should be masked")
	}
	if mask.Masked(56.0, 8.0) || mask.Masked(55.0, 9.0) {
		t.Fatal("moving or unknown-speed ships shouldn't create a port")
	}
}
