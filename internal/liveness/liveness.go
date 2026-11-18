package liveness

import (
	"sync"
	"time"
)

// Tracker remembers which minutes the feed delivered at least one message.
type Tracker struct {
	mu      sync.Mutex
	minutes map[int64]struct{}
	keep    int64 // minutes of history to keep
}

func New(keep time.Duration) *Tracker {
	return &Tracker{minutes: map[int64]struct{}{}, keep: int64(keep / time.Minute)}
}

// Mark records that the feed was alive in ts's minute.
func (t *Tracker) Mark(ts time.Time) {
	m := ts.Unix() / 60
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.minutes[m]; ok {
		return // already marked: the common case, so keep it cheap
	}
	t.minutes[m] = struct{}{}
	for k := range t.minutes { // new minute: drop ones too old to matter
		if k < m-t.keep {
			delete(t.minutes, k)
		}
	}
}

// Alive reports whether the feed delivered anything in that minute.
func (t *Tracker) Alive(minute int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.minutes[minute]
	return ok
}
