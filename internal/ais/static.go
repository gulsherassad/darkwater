package ais

import (
	"encoding/json"
	"strings"
	"time"
)

// ShipInfo is a ship's "ID card" (AIS ShipStaticData, sent every ~6 min).
type ShipInfo struct {
	MMSI        int64     `json:"mmsi"`
	Name        string    `json:"name"`
	Type        int       `json:"type"`     // AIS ship type code, 0 = unknown
	DraughtM    float64   `json:"draughtM"` // how deep the hull sits, 0 = unknown
	LengthM     float64   `json:"lengthM"`
	Destination string    `json:"destination"`
	Updated     time.Time `json:"updated"`
}

type staticEnvelope struct {
	MessageType string `json:"MessageType"`
	MetaData    struct {
		MMSI int64 `json:"MMSI"`
	} `json:"MetaData"`
	Message struct {
		ShipStaticData *struct {
			Name                 string  `json:"Name"`
			Type                 int     `json:"Type"`
			MaximumStaticDraught float64 `json:"MaximumStaticDraught"`
			Destination          string  `json:"Destination"`
			Dimension            struct {
				A float64 `json:"A"` // antenna to bow
				B float64 `json:"B"` // antenna to stern
			} `json:"Dimension"`
		} `json:"ShipStaticData"`
	} `json:"Message"`
}

// ParseStatic decodes a ShipStaticData message. ok is false for any other message.
func ParseStatic(msg []byte, now time.Time) (ShipInfo, bool) {
	var env staticEnvelope
	if err := json.Unmarshal(msg, &env); err != nil || env.MessageType != "ShipStaticData" {
		return ShipInfo{}, false
	}
	s := env.Message.ShipStaticData
	if s == nil || env.MetaData.MMSI == 0 {
		return ShipInfo{}, false
	}
	return ShipInfo{
		MMSI:        env.MetaData.MMSI,
		Name:        clean(s.Name),
		Type:        s.Type,
		DraughtM:    s.MaximumStaticDraught,
		LengthM:     s.Dimension.A + s.Dimension.B,
		Destination: clean(s.Destination),
		Updated:     now,
	}, true
}

// AIS pads text fields with '@' and spaces
func clean(s string) string {
	return strings.TrimSpace(strings.TrimRight(s, "@ "))
}

// IsService: towing, military, pilot, rescue, tug, port tender, law enforcement, medical...
func (s ShipInfo) IsService() bool {
	t := s.Type
	return t == 31 || t == 32 || t == 35 || (t >= 50 && t <= 59)
}

// CarriesCargo: cargo ships (70-79) and tankers (80-89)
func (s ShipInfo) CarriesCargo() bool {
	return s.Type >= 70 && s.Type <= 89
}
