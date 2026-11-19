package detect

import (
	"testing"
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
)

// simulate scans two ships once a minute: A fixed at 55.5,11.0 and B at latB.
func simulate(d *EncounterDetector, from, to int, latB, kn float64) {
	for m := from; m <= to; m++ {
		a := pos(219000010, float64(m), 55.5, 11.0)
		b := pos(219000011, float64(m), latB, 11.0)
		a.SpeedKn, b.SpeedKn = kn, kn
		d.Scan(t0.Add(time.Duration(m)*time.Minute), []ais.Position{a, b})
	}
}

func encEvents(d *EncounterDetector) *[]EncounterEvent {
	var got []EncounterEvent
	d.OnEvent = func(e EncounterEvent) { got = append(got, e) }
	return &got
}

func TestAlongsideForAnHour(t *testing.T) {
	d := NewEncounterDetector()
	got := encEvents(d)
	simulate(d, 0, 60, 55.5004, 0.5) // ~45 m apart for an hour
	if len(*got) != 1 || (*got)[0].Kind != EncounterStart || (*got)[0].Minutes < 30 {
		t.Fatalf("want one encounter_start after 30 min, got %+v", *got)
	}
	simulate(d, 61, 100, 55.6, 0.5) // B leaves (~11 km away)
	last := (*got)[len(*got)-1]
	if len(*got) != 2 || last.Kind != EncounterEnd || last.Minutes < 59 || last.Minutes > 61 {
		t.Fatalf("want one encounter_end of ~60 min, got %+v", *got)
	}
}

func TestBriefMeetingIgnored(t *testing.T) {
	d := NewEncounterDetector()
	got := encEvents(d)
	simulate(d, 0, 20, 55.5004, 0.5) // together 20 min only
	simulate(d, 21, 80, 55.6, 0.5)
	if len(*got) != 0 {
		t.Fatalf("a 20-minute meeting shouldn't count: %+v", *got)
	}
}

func TestFastShipsIgnored(t *testing.T) {
	d := NewEncounterDetector()
	got := encEvents(d)
	simulate(d, 0, 60, 55.5004, 12)
	if len(*got) != 0 {
		t.Fatalf("ships doing 12 kn aren't transferring cargo: %+v", *got)
	}
}

func TestPortMaskSuppresses(t *testing.T) {
	mask := NewPortMask()
	for i := int64(0); i < 3; i++ {
		p := pos(219000100+i, 0, 55.5, 11.0)
		p.NavStatus = navMoored
		mask.Add(p)
	}
	if n := mask.Finish(3); n != 1 {
		t.Fatalf("want 1 port square, got %d", n)
	}
	if !mask.Masked(55.515, 11.0) {
		t.Fatal("neighbouring square should be masked too")
	}

	d := NewEncounterDetector()
	d.Masked = mask.Masked
	got := encEvents(d)
	simulate(d, 0, 60, 55.5004, 0.5)
	if len(*got) != 0 {
		t.Fatalf("ships in a port shouldn't be flagged: %+v", *got)
	}
}
