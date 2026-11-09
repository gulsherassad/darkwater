package tracker

import (
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
)

// Store keeps the latest known position of every ship, keyed by MMSI.
type Store struct {
	ships map[int64]ais.Position
}

func New() *Store {
	return &Store{ships: make(map[int64]ais.Position)}
}

// Update saves p if it's newer than what we have.
// Returns the time since this ship's last report (0 if it's a new ship),
// and false if the message is stale or a duplicate.
func (s *Store) Update(p ais.Position) (time.Duration, bool) {
	old, exists := s.ships[p.MMSI]
	if exists && !p.Time.After(old.Time) {
		return 0, false
	}
	s.ships[p.MMSI] = p
	if !exists {
		return 0, true
	}
	return p.Time.Sub(old.Time), true
}

func (s *Store) Len() int {
	return len(s.ships)
}
