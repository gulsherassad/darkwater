package track

import (
	"io"
	"os"

	"github.com/gulsherassad/darkwater/internal/dma"
	"github.com/gulsherassad/darkwater/internal/grid"
)

// LoadDMA builds per-minute tracks for every ship in the study area.
func LoadDMA(path string) ([]*Track, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rd, err := dma.NewReader(f)
	if err != nil {
		return nil, err
	}
	b := NewBuilder()
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
		b.Add(p)
	}
	return b.Tracks(), nil
}
