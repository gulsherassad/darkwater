package vessels

import (
	"sync"

	"github.com/gulsherassad/darkwater/internal/ais"
)

// Registry holds the latest ID card for every ship. Safe for concurrent use.
type Registry struct {
	mu    sync.RWMutex
	ships map[int64]ais.ShipInfo
}

func New() *Registry {
	return &Registry{ships: map[int64]ais.ShipInfo{}}
}

func (r *Registry) Update(s ais.ShipInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ships[s.MMSI] = s
}

func (r *Registry) Get(mmsi int64) (ais.ShipInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.ships[mmsi]
	return s, ok
}

func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.ships)
}
