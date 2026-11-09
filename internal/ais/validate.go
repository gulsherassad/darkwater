package ais

// Validate returns "" if the position is usable, otherwise a reason for rejecting it.
func (p Position) Validate() string {
	if p.MMSI < 200000000 || p.MMSI > 799999999 {
		return "bad_mmsi"
	}
	if p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180 {
		return "no_position"
	}
	if p.Lat == 0 && p.Lon == 0 {
		return "null_island"
	}
	return ""
}
