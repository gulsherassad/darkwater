package dma

import (
	"io"
	"strings"
	"testing"
	"time"
)

const sample = `# Timestamp,Type of mobile,MMSI,Latitude,Longitude,Navigational status,ROT,SOG,COG,Heading,IMO,Callsign,Name,Ship type,Cargo type,Width,Length,Type of position fixing device,Draught,Destination,ETA,Data source type,A,B,C,D
09/11/2026 00:00:00,Class A,219142000,56.331198,7.855322,Under way using engine,,1.0,3.1,256,Unknown,Unknown,TEST SHIP,Undefined,,,,Undefined,,Unknown,,AIS,,,,
09/11/2026 00:00:01,Base Station,2190064,55.0,10.0,Unknown value,,,,,Unknown,Unknown,,Undefined,,,,Undefined,,Unknown,,AIS,,,,
09/11/2026 00:00:02,Class B,211000000,54.5,11.0,Unknown value,,,,511,Unknown,Unknown,,Undefined,,,,Undefined,,Unknown,,AIS,,,,
`

func TestReader(t *testing.T) {
	r, err := NewReader(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}

	// Row 1: a normal Class A ship
	p, reason, err := r.Next()
	if err != nil || reason != "" {
		t.Fatalf("row 1: reason=%q err=%v", reason, err)
	}
	want := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	if p.MMSI != 219142000 || p.SpeedKn != 1.0 || p.Heading != 256 || p.Name != "TEST SHIP" || !p.Time.Equal(want) {
		t.Fatalf("row 1 parsed wrong: %+v", p)
	}

	// Row 2: base station, should be skipped
	if _, reason, _ := r.Next(); reason != "not_ship" {
		t.Fatalf("row 2: want not_ship, got %q", reason)
	}

	// Row 3: Class B with empty speed/course and heading 511
	p, reason, _ = r.Next()
	if reason != "" || p.SpeedKn != -1 || p.CourseDeg != -1 || p.Heading != -1 {
		t.Fatalf("row 3: missing values not handled: reason=%q %+v", reason, p)
	}

	// Then end of file
	if _, _, err := r.Next(); err != io.EOF {
		t.Fatalf("want io.EOF, got %v", err)
	}
}
