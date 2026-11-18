package liveness

import (
	"testing"
	"time"
)

func TestMarkAndPrune(t *testing.T) {
	tr := New(60 * time.Minute)
	start := time.Date(2026, 11, 12, 19, 0, 0, 0, time.UTC)

	tr.Mark(start)
	if !tr.Alive(start.Unix() / 60) {
		t.Fatal("marked minute should be alive")
	}
	if tr.Alive(start.Unix()/60 + 1) {
		t.Fatal("unmarked minute should not be alive")
	}

	tr.Mark(start.Add(2 * time.Hour)) // well past the 60-minute window
	if tr.Alive(start.Unix() / 60) {
		t.Fatal("old minute should have been pruned")
	}
}
