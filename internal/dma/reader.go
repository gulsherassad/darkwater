package dma

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
)

// Column positions in the DMA CSV
const (
	colTime    = 0
	colType    = 1
	colMMSI    = 2
	colLat     = 3
	colLon     = 4
	colStatus  = 5
	colSpeed   = 7
	colCourse  = 8
	colHeading = 9
	colName    = 12
)

const timeLayout = "02/01/2006 15:04:05" // day/month/year

// Navigational status text -> standard AIS code
var navCodes = map[string]int{
	"Under way using engine":              0,
	"At anchor":                           1,
	"Not under command":                   2,
	"Restricted manoeuverability":         3,
	"Restricted maneuverability":          3,
	"Constrained by her draught":          4,
	"Moored":                              5,
	"Aground":                             6,
	"Engaged in fishing":                  7,
	"Under way sailing":                   8,
	"Reserved for future amendment [HSC]": 9,  // high-speed craft
	"Reserved for future amendment [WIG]": 10, // wing-in-ground craft
	"Power-driven vessel towing astern":   11,
	"Power-driven vessel pushing ahead or towing alongside": 12,
	"Reserved for future use":                               13,
	"Unknown value":                                         15,
}

// Reader streams ais.Positions out of a DMA CSV, one row at a time.
type Reader struct {
	r *csv.Reader
	// Status strings we didn't recognise, so we can add them later
	UnknownStatus map[string]int
}

func NewReader(rd io.Reader) (*Reader, error) {
	cr := csv.NewReader(bufio.NewReaderSize(rd, 1<<20)) // 1 MB read buffer
	cr.ReuseRecord = true                               // reuse memory between rows
	cr.FieldsPerRecord = -1                             // tolerate rows with odd column counts
	cr.LazyQuotes = true                                // tolerate stray quotes in ship names

	if _, err := cr.Read(); err != nil { // skip the header row
		return nil, fmt.Errorf("read header: %w", err)
	}
	return &Reader{r: cr, UnknownStatus: map[string]int{}}, nil
}

// Next returns the next row as a Position.
// If reason is non-empty, the row was skipped and reason says why.
// err is io.EOF at the end of the file.
func (r *Reader) Next() (p ais.Position, reason string, err error) {
	rec, err := r.r.Read()
	if err == io.EOF {
		return p, "", io.EOF
	}
	if err != nil {
		var pe *csv.ParseError
		if errors.As(err, &pe) {
			return p, "bad_csv", nil // one broken row shouldn't stop the whole file
		}
		return p, "", err
	}
	if len(rec) <= colName {
		return p, "short_row", nil
	}
	if typ := rec[colType]; typ != "Class A" && typ != "Class B" {
		return p, "not_ship", nil // base stations, buoys, aircraft
	}

	t, err := time.ParseInLocation(timeLayout, rec[colTime], time.UTC)
	if err != nil {
		return p, "bad_time", nil
	}
	mmsi, err := strconv.ParseInt(rec[colMMSI], 10, 64)
	if err != nil {
		return p, "bad_mmsi_field", nil
	}
	lat, err1 := strconv.ParseFloat(rec[colLat], 64)
	lon, err2 := strconv.ParseFloat(rec[colLon], 64)
	if err1 != nil || err2 != nil {
		return p, "bad_position", nil
	}

	speed := floatOr(rec[colSpeed], -1)
	if speed >= 102.2 {
		speed = -1
	}
	course := floatOr(rec[colCourse], -1)
	if course >= 360 {
		course = -1
	}
	heading := int(floatOr(rec[colHeading], -1))
	if heading >= 360 { // includes 511 = unknown
		heading = -1
	}

	return ais.Position{
		MMSI:      mmsi,
		Name:      strings.TrimSpace(rec[colName]),
		Time:      t,
		Lat:       lat,
		Lon:       lon,
		SpeedKn:   speed,
		CourseDeg: course,
		Heading:   heading,
		NavStatus: r.navCode(rec[colStatus]),
	}, "", nil
}

func (r *Reader) navCode(s string) int {
	if c, ok := navCodes[s]; ok {
		return c
	}
	r.UnknownStatus[s]++
	return 15 // "not defined"
}

// floatOr parses s, or returns def if s is empty or not a number.
func floatOr(s string, def float64) float64 {
	if s == "" {
		return def
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return v
}
