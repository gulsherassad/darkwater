package reception

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"

	h3 "github.com/uber/h3-go/v4"
)

// Counts for one hexagon: ship-minutes DMA saw there, and how many the feed heard.
type Counts struct {
	Present, Heard int
}

// Model: per-hexagon chance that the feed hears a present ship in a given minute.
type Model struct {
	Global   float64 // overall hearing rate, used as the prior
	Strength float64 // how many "pretend observations" the prior is worth
	cells    map[h3.Cell]Counts
}

func Fit(counts map[h3.Cell]Counts, strength float64) *Model {
	present, heard := 0, 0
	for _, c := range counts {
		present += c.Present
		heard += c.Heard
	}
	global := 0.0
	if present > 0 {
		global = float64(heard) / float64(present)
	}
	return &Model{Global: global, Strength: strength, cells: counts}
}

// Prob is the chance the feed hears a ship in cell c during one minute.
// Cells with little data are pulled toward the global rate.
func (m *Model) Prob(c h3.Cell) float64 {
	cnt := m.cells[c]
	return (float64(cnt.Heard) + m.Strength*m.Global) / (float64(cnt.Present) + m.Strength)
}

// SilenceLogProb is the log of the chance of hearing nothing for one minute
// in each of the given cells (one entry per silent minute).
// Logs are added instead of multiplying probabilities, which avoids
// tiny numbers rounding to zero.
func (m *Model) SilenceLogProb(cells []h3.Cell) float64 {
	sum := 0.0
	for _, c := range cells {
		p := math.Min(m.Prob(c), 0.999)
		sum += math.Log(1 - p)
	}
	return sum
}

// Save writes every cell's counts and probability to CSV, sorted for reproducibility.
func (m *Model) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	cells := make([]h3.Cell, 0, len(m.cells))
	for c := range m.cells {
		cells = append(cells, c)
	}
	sort.Slice(cells, func(i, j int) bool { return cells[i] < cells[j] })

	w := csv.NewWriter(f)
	w.Write([]string{"cell", "present", "heard", "prob"})
	for _, c := range cells {
		cnt := m.cells[c]
		w.Write([]string{
			c.String(),
			strconv.Itoa(cnt.Present),
			strconv.Itoa(cnt.Heard),
			fmt.Sprintf("%.4f", m.Prob(c)),
		})
	}
	w.Flush()
	return w.Error()
}
