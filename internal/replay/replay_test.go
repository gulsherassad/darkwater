package replay

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gulsherassad/darkwater/internal/ais"
)

func TestReadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.jsonl") // temp folder, deleted after the test
	lines := `{"Message":{"CompressionEnabled":false},"MessageType":"SubscriptionConfirmation"}
{"MetaData":{"MMSI":266145000,"ShipName":"GLITTFORS","time_utc":"2026-11-09 19:15:45.558233139 +0000 UTC"},"MessageType":"PositionReport","Message":{"PositionReport":{"Latitude":57.05873,"Longitude":12.2708,"Sog":0,"Cog":294.3,"TrueHeading":511,"NavigationalStatus":7}}}
not json at all
`
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	var got []ais.Position
	st, err := ReadFile(path, func(p ais.Position) { got = append(got, p) })
	if err != nil {
		t.Fatal(err)
	}
	if st.Lines != 3 || st.Positions != 1 || st.Skipped != 2 {
		t.Fatalf("stats wrong: %+v", st)
	}
	if got[0].MMSI != 266145000 || got[0].Heading != -1 || got[0].Name != "GLITTFORS" {
		t.Fatalf("position wrong: %+v", got[0])
	}
}
