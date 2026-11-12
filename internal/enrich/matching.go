package enrich

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	gtfs "github.com/patrickbr/gtfsparser/gtfs"

	"github.com/patrickbr/gtfsparser"
)

// tieMeters: candidates that are equally good by ref and name and at most this
// much further than the best one count as equally near.
const tieMeters = 5.0

const cellDeg = 0.002 // grid cell size, ~220 m

// transportKinds are the OSM feature kinds that can match a boarding stop.
var transportKinds = map[string]bool{
	"bus": true, "rail": true, "tram": true, "subway": true, "ferry": true, "aerialway": true,
}

// routeKind maps a basic or extended route_type to a transport mode as used
// by osmfeat ("" if unknown).
func routeKind(t int16) string {
	switch {
	case t == 3 || (t >= 700 && t < 800):
		return "bus"
	case t == 2 || (t >= 100 && t < 200):
		return "rail"
	case t == 0 || (t >= 900 && t < 1000):
		return "tram"
	case t == 1 || (t >= 400 && t < 500):
		return "subway"
	case t == 4 || (t >= 1000 && t < 1100):
		return "ferry"
	case t == 5 || t == 6 || t == 7 || (t >= 1300 && t < 1500):
		return "aerialway"
	}
	return ""
}

// stopKinds returns, per stop ID, the set of transport modes of the routes
// that serve the stop.
func stopKinds(feed *gtfsparser.Feed) map[string]map[string]bool {
	out := make(map[string]map[string]bool)
	for _, t := range feed.Trips {
		if t.Route == nil {
			continue
		}
		kind := routeKind(t.Route.Type)
		if kind == "" {
			continue
		}
		for i := range t.StopTimes {
			s := t.StopTimes[i].Stop()
			if s == nil {
				continue
			}
			if out[s.Id] == nil {
				out[s.Id] = make(map[string]bool)
			}
			out[s.Id][kind] = true
		}
	}
	return out
}

type cell struct{ y, x int }

func cellOf(lat, lon float64) cell {
	return cell{int(math.Floor(lat / cellDeg)), int(math.Floor(lon / cellDeg))}
}

// distM is an equirectangular distance in meters, good enough below 1 km.
func distM(lat1, lon1, lat2, lon2 float64) float64 {
	const mPerDeg = 111320.0
	dy := (lat2 - lat1) * mPerDeg
	dx := (lon2 - lon1) * mPerDeg * math.Cos((lat1+lat2)/2*math.Pi/180)
	return math.Hypot(dx, dy)
}

func normName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func namesMatch(a, b string) bool {
	a, b = normName(a), normName(b)
	return a != "" && b != "" && (a == b || strings.Contains(a, b) || strings.Contains(b, a))
}

type candidate struct {
	f        osmfeat.Feature
	dist     float64
	refMatch bool
	nameOK   bool
}

// better reports whether a ranks strictly before b (ref, name, distance).
func better(a, b candidate) bool {
	if a.refMatch != b.refMatch {
		return a.refMatch
	}
	if a.nameOK != b.nameOK {
		return a.nameOK
	}
	return a.dist < b.dist
}

// StopMatch is the result of matching one boarding stop to OSM features.
type StopMatch struct {
	Stop  *gtfs.Stop
	Modes map[string]bool // transport modes of the routes serving the stop
	// HasBest is false if no OSM feature of a fitting mode lies within the
	// match radius.
	HasBest bool
	Best    candidate
	// Tied are the candidates ranked as good as Best (same ref and name match,
	// at most tieMeters further away), excluding Best itself.
	Tied []candidate
}

// WheelchairConflict reports whether equally good candidates disagree on a
// definite wheelchair value.
func (m StopMatch) WheelchairConflict() bool {
	val := wheelchairValue(m.Best.f.Wheelchair)
	if !m.HasBest || val == 0 {
		return false
	}
	for _, c := range m.Tied {
		if v := wheelchairValue(c.f.Wheelchair); v != 0 && v != val {
			return true
		}
	}
	return false
}

// Match matches every boarding stop (location_type 0, with coordinates other
// than (0,0), served by at least one route) to the OSM transport features
// within the radius of the same transport mode. Stop positions and features
// that are not transport stops never match. The result is sorted by stop ID.
func Match(feed *gtfsparser.Feed, feats []osmfeat.Feature, opts WheelchairOptions) []StopMatch {
	index := make(map[cell][]int, len(feats))
	for i, f := range feats {
		if f.StopPos || !transportKinds[f.Kind] {
			continue
		}
		c := cellOf(f.Lat, f.Lon)
		index[c] = append(index[c], i)
	}
	kinds := stopKinds(feed)

	ids := make([]string, 0, len(feed.Stops))
	for id := range feed.Stops {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var out []StopMatch
	for _, id := range ids {
		s := feed.Stops[id]
		if s.Location_type != 0 || !s.HasLatLon() || (s.Lat == 0 && s.Lon == 0) {
			continue
		}
		modes := kinds[id]
		if len(modes) == 0 {
			continue
		}
		lat, lon := float64(s.Lat), float64(s.Lon)
		c0 := cellOf(lat, lon)

		var cands []candidate
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				for _, i := range index[cell{c0.y + dy, c0.x + dx}] {
					f := feats[i]
					if !modes[f.Kind] {
						continue
					}
					d := distM(lat, lon, f.Lat, f.Lon)
					limit := opts.RadiusM
					if f.Area {
						limit = opts.AreaRadiusM
					}
					if d > limit {
						continue
					}
					cands = append(cands, candidate{
						f: f, dist: d,
						refMatch: s.Platform_code != "" && f.Ref == s.Platform_code,
						nameOK:   namesMatch(s.Name, f.Name),
					})
				}
			}
		}
		m := StopMatch{Stop: s, Modes: modes}
		if len(cands) > 0 {
			best := 0
			for i := 1; i < len(cands); i++ {
				if better(cands[i], cands[best]) {
					best = i
				}
			}
			m.HasBest, m.Best = true, cands[best]
			for i, c := range cands {
				if i != best && c.refMatch == m.Best.refMatch && c.nameOK == m.Best.nameOK && c.dist-m.Best.dist <= tieMeters {
					m.Tied = append(m.Tied, c)
				}
			}
		}
		out = append(out, m)
	}
	return out
}
