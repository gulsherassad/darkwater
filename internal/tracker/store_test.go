package tracker

import (
	"sync"
	"testing"
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
)

// 4 writers and 1 reader hit the store at the same time.
func TestConcurrentUpdateAndSnapshot(t *testing.T) {
	s := New()
	start := time.Now()
	var wg sync.WaitGroup

	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				s.Update(ais.Position{
					MMSI: int64(200000000 + w*1000 + i%100), // 100 ships per writer
					Time: start.Add(time.Duration(i) * time.Second),
				})
			}
		}(w)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = s.Snapshot()
		}
	}()

	wg.Wait()
	if got := s.Len(); got != 400 {
		t.Fatalf("want 400 ships, got %d", got)
	}
}

// An older or identical report must not overwrite a newer one.
func TestUpdateRejectsStale(t *testing.T) {
	s := New()
	now := time.Now()
	s.Update(ais.Position{MMSI: 211000000, Time: now})

	if _, ok := s.Update(ais.Position{MMSI: 211000000, Time: now.Add(-time.Minute)}); ok {
		t.Fatal("older report should be rejected")
	}
	if _, ok := s.Update(ais.Position{MMSI: 211000000, Time: now}); ok {
		t.Fatal("duplicate report should be rejected")
	}
}
