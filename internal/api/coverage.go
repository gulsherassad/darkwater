package api

import (
	"encoding/csv"
	"errors"
	"net/http"
	"os"
	"strconv"
)

// coverageCell is one hexagon's counts, as the browser sees it.
type coverageCell struct {
	Cell      string `json:"cell"`
	DMAMsgs   int    `json:"dmaMsgs"`
	DMAShips  int    `json:"dmaShips"`
	FeedMsgs  int    `json:"feedMsgs"`
	FeedShips int    `json:"feedShips"`
}

// coverageHandler serves the CSV produced by cmd/cells as JSON.
func coverageHandler(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cells, err := loadCoverage(path)
		if err != nil {
			http.Error(w, "coverage not available (run cmd/cells first)", http.StatusNotFound)
			return
		}
		writeJSON(w, cells)
	}
}

func loadCoverage(path string) ([]coverageCell, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rows, err := csv.NewReader(f).ReadAll() // small file (~4k rows), fine to load at once
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, errors.New("coverage file is empty")
	}

	num := func(s string) int {
		v, _ := strconv.Atoi(s)
		return v
	}

	out := make([]coverageCell, 0, len(rows)-1)
	for _, r := range rows[1:] { // skip header
		if len(r) != 5 {
			continue
		}
		out = append(out, coverageCell{
			Cell:      r[0],
			DMAMsgs:   num(r[1]),
			DMAShips:  num(r[2]),
			FeedMsgs:  num(r[3]),
			FeedShips: num(r[4]),
		})
	}
	return out, nil
}
