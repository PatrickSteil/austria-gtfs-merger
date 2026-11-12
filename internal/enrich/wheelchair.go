package enrich

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/patrickbr/gtfsparser"
)

// WheelchairOptions are the match radii in meters.
type WheelchairOptions struct {
	// RadiusM applies to point features, AreaRadiusM to ways/polygons
	// (platform areas), whose centroid can be further from the stop.
	RadiusM, AreaRadiusM float64
}

// DefaultWheelchairOptions returns the radii from the design (30 m / 40 m).
func DefaultWheelchairOptions() WheelchairOptions {
	return WheelchairOptions{RadiusM: 30, AreaRadiusM: 40}
}

// tieMeters: candidates that are equally good by ref and name and at most this
// much further than the best one count as equally near.
const tieMeters = 5.0

const cellDeg = 0.002 // grid cell size, ~220 m

// wheelchairValue maps a normalized OSM wheelchair value to GTFS
// wheelchair_boarding. Only a definite yes or no is asserted.
func wheelchairValue(v string) int8 {
	switch v {
	case "yes":
		return 1
	case "no":
		return 2
	}
	return 0
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

// WheelchairKnown counts stops with a wheelchair_boarding value other than 0.
func WheelchairKnown(feed *gtfsparser.Feed) int {
	n := 0
	for _, s := range feed.Stops {
		if s.Wheelchair_boarding != 0 {
			n++
		}
	}
	return n
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

// Wheelchair derives wheelchair_boarding for boarding stops from OSM features
// and then for parent stations from their children. Source values are kept;
// when in doubt the value stays 0 (unknown).
func Wheelchair(feed *gtfsparser.Feed, feats []osmfeat.Feature, opts WheelchairOptions) Report {
	rep := Report{Step: "wheelchair"}

	index := make(map[cell][]int, len(feats))
	for i, f := range feats {
		c := cellOf(f.Lat, f.Lon)
		index[c] = append(index[c], i)
	}
	kinds := stopKinds(feed)

	ids := make([]string, 0, len(feed.Stops))
	for id := range feed.Stops {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		s := feed.Stops[id]
		if s.Location_type != 0 || s.Wheelchair_boarding != 0 || !s.HasLatLon() || (s.Lat == 0 && s.Lon == 0) {
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
		if len(cands) == 0 {
			continue
		}
		best := cands[0]
		for _, c := range cands[1:] {
			if better(c, best) {
				best = c
			}
		}
		val := wheelchairValue(best.f.Wheelchair)
		conflict := false
		for _, c := range cands {
			if c.refMatch == best.refMatch && c.nameOK == best.nameOK && c.dist-best.dist <= tieMeters &&
				wheelchairValue(c.f.Wheelchair) != val && wheelchairValue(c.f.Wheelchair) != 0 && val != 0 {
				conflict = true
			}
		}
		if conflict {
			rep.Conflict("%s (%s): conflicting OSM features nearby", s.Id, s.Name)
			continue
		}
		if val == 0 {
			continue
		}
		s.Wheelchair_boarding = val
		rep.Changed++
		kind := "?"
		for _, k := range []string{"bus", "rail", "tram", "subway", "ferry", "aerialway"} {
			if modes[k] {
				kind = k
				break
			}
		}
		if val == 1 {
			rep.Count(kind+"_yes", 1)
		} else {
			rep.Count(kind+"_no", 1)
		}
		rep.Example("%s (%s): %d from OSM at %.0f m", s.Id, s.Name, val, best.dist)
	}

	deriveParents(feed, ids)
	return rep
}

// deriveParents sets wheelchair_boarding on stations without a value: 1 if
// any child is accessible, 2 if all children with a known value are not.
func deriveParents(feed *gtfsparser.Feed, ids []string) {
	type agg struct{ yes, no int }
	sums := make(map[string]*agg)
	for _, id := range ids {
		s := feed.Stops[id]
		if s.Parent_station == nil || s.Wheelchair_boarding == 0 {
			continue
		}
		a := sums[s.Parent_station.Id]
		if a == nil {
			a = &agg{}
			sums[s.Parent_station.Id] = a
		}
		if s.Wheelchair_boarding == 1 {
			a.yes++
		} else {
			a.no++
		}
	}
	for _, id := range ids {
		p := feed.Stops[id]
		if p.Location_type != 1 || p.Wheelchair_boarding != 0 {
			continue
		}
		switch a := sums[p.Id]; {
		case a == nil:
		case a.yes > 0:
			p.Wheelchair_boarding = 1
		default:
			p.Wheelchair_boarding = 2
		}
	}
}
