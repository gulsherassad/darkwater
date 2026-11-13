package geo

import "math"

const EarthRadiusKm = 6371.0088

func rad(d float64) float64 { return d * math.Pi / 180 }
func deg(r float64) float64 { return r * 180 / math.Pi }

// HaversineKm is the distance between two points along the Earth's surface.
func HaversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := rad(lat2 - lat1)
	dLon := rad(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * EarthRadiusKm * math.Asin(math.Sqrt(a))
}

// Destination: start at (lat, lon), travel distKm on a compass bearing.
// Returns where you end up.
func Destination(lat, lon, bearingDeg, distKm float64) (float64, float64) {
	d := distKm / EarthRadiusKm // distance as an angle
	b := rad(bearingDeg)
	lat1, lon1 := rad(lat), rad(lon)

	lat2 := math.Asin(math.Sin(lat1)*math.Cos(d) + math.Cos(lat1)*math.Sin(d)*math.Cos(b))
	lon2 := lon1 + math.Atan2(
		math.Sin(b)*math.Sin(d)*math.Cos(lat1),
		math.Cos(d)-math.Sin(lat1)*math.Sin(lat2),
	)
	return deg(lat2), deg(lon2)
}
