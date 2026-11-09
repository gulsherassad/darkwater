package ais

import (
	"strings"
	"time"
)

// What AISStream sends (only the fields we need)
type Envelope struct {
	MessageType string   `json:"MessageType"`
	MetaData    MetaData `json:"MetaData"`
	Message     struct {
		PositionReport *PositionReport `json:"PositionReport"`
	} `json:"Message"`
}

type MetaData struct {
	MMSI     int64  `json:"MMSI"`
	ShipName string `json:"ShipName"`
	TimeUTC  string `json:"time_utc"`
}

type PositionReport struct {
	Latitude           float64 `json:"Latitude"`
	Longitude          float64 `json:"Longitude"`
	Sog                float64 `json:"Sog"`         // speed, knots
	Cog                float64 `json:"Cog"`         // direction of travel, degrees
	TrueHeading        int     `json:"TrueHeading"` // 511 = unknown
	NavigationalStatus int     `json:"NavigationalStatus"`
}

// Our clean version: what the rest of the app uses
type Position struct {
	MMSI      int64
	Name      string
	Time      time.Time
	Lat, Lon  float64
	SpeedKn   float64
	CourseDeg float64
	Heading   int // -1 = unknown
	NavStatus int
}

const timeLayout = "2006-01-02 15:04:05.999999999 -0700 MST"

// ToPosition translates an envelope into a clean Position.
// The bool is false if this message isn't a usable position report.
func (e Envelope) ToPosition() (Position, bool) {
	pr := e.Message.PositionReport
	if e.MessageType != "PositionReport" || pr == nil {
		return Position{}, false
	}
	t, err := time.Parse(timeLayout, e.MetaData.TimeUTC)
	if err != nil {
		return Position{}, false
	}
	heading := pr.TrueHeading
	if heading == 511 {
		heading = -1
	}
	speed := pr.Sog
	if speed >= 102.2 { // 102.3 = speed unknown
		speed = -1
	}
	course := pr.Cog
	if course >= 360 { // 360 = course unknown
		course = -1
	}
	return Position{
		MMSI:      e.MetaData.MMSI,
		Name:      strings.TrimSpace(e.MetaData.ShipName),
		Time:      t,
		Lat:       pr.Latitude,
		Lon:       pr.Longitude,
		SpeedKn:   speed,
		CourseDeg: course,
		Heading:   heading,
		NavStatus: pr.NavigationalStatus,
	}, true
}
