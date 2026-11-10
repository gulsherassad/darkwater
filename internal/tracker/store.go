package tracker

import (
	"sync"
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
)

// Store keeps the latest known position of every ship, keyed by MMSI.
// It's safe to use from multiple goroutines at once.
type Store struct {
	mu    sync.RWMutex
	ships map[int64]ais.Position
	dirty map[int64]struct{} // ships changed since the last TakeChanges
}

func New() *Store {
	return &Store{
		ships: make(map[int64]ais.Position),
		dirty: make(map[int64]struct{}),
	}
}

// Update saves p if it's newer than what we have.
// Returns the time since this ship's last report (0 if it's a new ship),
// and false if the message is stale or a duplicate.
func (s *Store) Update(p ais.Position) (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	old, exists := s.ships[p.MMSI]
	if exists && !p.Time.After(old.Time) {
		return 0, false
	}
	s.ships[p.MMSI] = p
	s.dirty[p.MMSI] = struct{}{}
	if !exists {
		return 0, true
	}
	return p.Time.Sub(old.Time), true
}

// Snapshot returns a copy of every ship's latest position.
func (s *Store) Snapshot() []ais.Position {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]ais.Position, 0, len(s.ships))
	for _, p := range s.ships {
		out = append(out, p)
	}
	return out
}

// TakeChanges returns ships updated since the last call, then clears the list.
func (s *Store) TakeChanges() []ais.Position {
	s.mu.Lock() // Lock, not RLock: we're modifying dirty
	defer s.mu.Unlock()

	out := make([]ais.Position, 0, len(s.dirty))
	for mmsi := range s.dirty {
		out = append(out, s.ships[mmsi])
	}
	s.dirty = make(map[int64]struct{})
	return out
}

func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.ships)
}
