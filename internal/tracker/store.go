package tracker

import "github.com/gulsherassad/darkwater/internal/ais"

// Store keeps the latest known position of every ship, keyed by MMSI.
type Store struct {
	ships map[int64]ais.Position
}

func New() *Store {
	return &Store{ships: make(map[int64]ais.Position)}
}

// Update saves p if it's newer than what we have.
// Returns false if it's stale or a duplicate.
func (s *Store) Update(p ais.Position) bool {
	old, exists := s.ships[p.MMSI]
	if exists && !p.Time.After(old.Time) {
		return false
	}
	s.ships[p.MMSI] = p
	return true
}

func (s *Store) Len() int {
	return len(s.ships)
}
