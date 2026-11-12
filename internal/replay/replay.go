package replay

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/gulsherassad/darkwater/internal/ais"
)

type Stats struct {
	Lines, Positions, Skipped int
}

// ReadFile streams one JSONL recording, calling fn for every valid position.
// It uses the same translator and validation as the live feed.
func ReadFile(path string, fn func(ais.Position)) (Stats, error) {
	var st Stats
	f, err := os.Open(path)
	if err != nil {
		return st, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20) // allow lines up to 1 MB

	for sc.Scan() {
		st.Lines++
		var env ais.Envelope
		if err := json.Unmarshal(sc.Bytes(), &env); err != nil {
			st.Skipped++ // e.g. a half-written last line
			continue
		}
		p, ok := env.ToPosition()
		if !ok || p.Validate() != "" {
			st.Skipped++ // confirmation messages, junk
			continue
		}
		st.Positions++
		fn(p)
	}
	if err := sc.Err(); err != nil {
		return st, fmt.Errorf("%s: %w", path, err)
	}
	return st, nil
}

// Files lists the recordings in dir, oldest first.
// Hourly file names (2026-11-09T19.jsonl) sort in time order.
func Files(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}
