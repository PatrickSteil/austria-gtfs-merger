package enrich

import (
	"fmt"
	"sort"
	"strings"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/patrickbr/gtfsparser"
	gtfs "github.com/patrickbr/gtfsparser/gtfs"
)

const (
	// routeStopRadiusM is how close a GTFS stop and an OSM route member must be
	// to count as the same stop.
	routeStopRadiusM = 40.0
	// minRouteOverlap is the share of the smaller stop set that must match.
	minRouteOverlap = 0.6
	// minSharedStops is the least number of matching stops.
	minSharedStops = 2
	// maxTripsPerRoute bounds the trips read per route to collect its stops.
	maxTripsPerRoute = 50
)

// RouteMatchResult lists how GTFS routes and OSM route relations were paired.
type RouteMatchResult struct {
	Matched         []string // GTFS route IDs with at least one matching relation
	Ambiguous       []string // matched routes whose relations disagree on colour or name
	UnmatchedRoutes []string // GTFS route IDs without a matching relation
	UnmatchedGroups []string // "<mode> <ref>" of relations no GTFS route matched
}

func normRef(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r != ' ' && r != '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type routePoint struct{ lat, lon float64 }

// OSMRoutes pairs GTFS routes with OSM route relations (same mode, same
// normalized ref, and at least minRouteOverlap of the smaller stop set within
// routeStopRadiusM) and takes colour and name from the paired relations:
//
//   - route_color/route_text_color if the route has no source colour (colours
//     the routes step assigned itself, listed in palette, count as unset) and
//     all paired relations that carry a colour agree;
//   - route_long_name "from – to" if it is empty and the relations name exactly
//     one from/to pair, or two reversed pairs (the pair of the lower OSM ID wins).
func OSMRoutes(feed *gtfsparser.Feed, rels []osmfeat.RouteRel, palette map[string]bool) (Report, RouteMatchResult) {
	rep := Report{Step: "osmroutes"}
	var res RouteMatchResult

	byKey := make(map[string][]*osmfeat.RouteRel)
	for i := range rels {
		r := &rels[i]
		if r.Ref == "" {
			continue
		}
		k := r.Mode + "|" + normRef(r.Ref)
		byKey[k] = append(byKey[k], r)
	}

	tripsByRoute := make(map[*gtfs.Route][]string)
	for id, t := range feed.Trips {
		if t.Route != nil {
			tripsByRoute[t.Route] = append(tripsByRoute[t.Route], id)
		}
	}

	ids := make([]string, 0, len(feed.Routes))
	for id := range feed.Routes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	usedGroups := make(map[string]bool)
	for _, id := range ids {
		r := feed.Routes[id]
		key := routeKind(r.Type) + "|" + normRef(r.Short_name)
		cands := byKey[key]
		if routeKind(r.Type) == "" || len(cands) == 0 {
			res.UnmatchedRoutes = append(res.UnmatchedRoutes, id)
			continue
		}
		stops := routeStops(feed, tripsByRoute[r])
		var paired []*osmfeat.RouteRel
		for _, c := range cands {
			if overlapOK(stops, c.Stops) {
				paired = append(paired, c)
			}
		}
		if len(paired) == 0 {
			res.UnmatchedRoutes = append(res.UnmatchedRoutes, id)
			continue
		}
		res.Matched = append(res.Matched, id)
		usedGroups[key] = true
		sort.Slice(paired, func(i, j int) bool { return paired[i].ID < paired[j].ID })

		colour, colourOK := agreedColour(paired)
		pair, _ := agreedPair(paired)
		if !colourOK {
			res.Ambiguous = append(res.Ambiguous, id)
			rep.Conflict("route %s (%s): OSM relations disagree on colour", id, r.Short_name)
		}

		changed := false
		if colour != "" && (colorUnset(r) || palette[id]) {
			r.Color, r.Text_color = colour, TextColorFor(colour)
			rep.Count("colored", 1)
			changed = true
		}
		if pair != "" && r.Long_name == "" {
			r.Long_name = pair
			rep.Count("named", 1)
			changed = true
		}
		rep.Count("matched", 1)
		if changed {
			rep.Changed++
			rep.Example("route %s (%s): colour %s name %q from relation %d", id, r.Short_name, colour, pair, paired[0].ID)
		}
	}
	if len(res.Ambiguous) > 0 {
		rep.Count("ambiguous", len(res.Ambiguous))
	}

	seen := map[string]bool{}
	for k, group := range byKey {
		if usedGroups[k] || seen[k] {
			continue
		}
		seen[k] = true
		res.UnmatchedGroups = append(res.UnmatchedGroups, fmt.Sprintf("%s %s", group[0].Mode, group[0].Ref))
	}
	sort.Strings(res.UnmatchedGroups)
	return rep, res
}

// routeStops collects the distinct stop coordinates of up to maxTripsPerRoute
// trips (lowest trip IDs first, for determinism).
func routeStops(feed *gtfsparser.Feed, tripIDs []string) []routePoint {
	sort.Strings(tripIDs)
	if len(tripIDs) > maxTripsPerRoute {
		tripIDs = tripIDs[:maxTripsPerRoute]
	}
	seen := make(map[string]bool)
	var out []routePoint
	for _, tid := range tripIDs {
		t := feed.Trips[tid]
		for i := range t.StopTimes {
			s := t.StopTimes[i].Stop()
			if s == nil || seen[s.Id] || !s.HasLatLon() {
				continue
			}
			seen[s.Id] = true
			out = append(out, routePoint{float64(s.Lat), float64(s.Lon)})
		}
	}
	return out
}

// overlapOK reports whether enough of the smaller stop set has a counterpart
// in the other within routeStopRadiusM. The matches are counted from the
// smaller set's side, so several dense stops next to one stop of the other set
// cannot push the share above what that set really covers.
func overlapOK(gtfsStops []routePoint, relStops []osmfeat.Point) bool {
	if len(gtfsStops) < minSharedStops || len(relStops) < minSharedStops {
		return false
	}
	shared, smaller := 0, 0
	if len(gtfsStops) <= len(relStops) {
		smaller = len(gtfsStops)
		for _, g := range gtfsStops {
			for _, p := range relStops {
				if distM(g.lat, g.lon, p.Lat, p.Lon) <= routeStopRadiusM {
					shared++
					break
				}
			}
		}
	} else {
		smaller = len(relStops)
		for _, p := range relStops {
			for _, g := range gtfsStops {
				if distM(g.lat, g.lon, p.Lat, p.Lon) <= routeStopRadiusM {
					shared++
					break
				}
			}
		}
	}
	return shared >= minSharedStops && float64(shared)/float64(smaller) >= minRouteOverlap
}

// agreedColour returns the colour all coloured relations agree on. If they
// disagree it returns ("", false); if none has a colour, ("", true).
func agreedColour(rels []*osmfeat.RouteRel) (string, bool) {
	colour := ""
	for _, r := range rels {
		c := osmfeat.NormalizeColour(r.Colour)
		if c == "" {
			continue
		}
		if colour == "" {
			colour = c
		} else if colour != c {
			return "", false
		}
	}
	return colour, true
}

// agreedPair returns "from – to" of the lowest-ID relation if the relations
// name exactly one from/to pair or two reversed pairs; otherwise "".
func agreedPair(rels []*osmfeat.RouteRel) (string, bool) {
	type pair struct{ from, to string }
	var pairs []pair
	first := ""
	for _, r := range rels {
		if r.From == "" || r.To == "" {
			continue
		}
		p := pair{r.From, r.To}
		known := false
		for _, q := range pairs {
			if q == p {
				known = true
			}
		}
		if !known {
			pairs = append(pairs, p)
		}
		if first == "" {
			first = r.From + " – " + r.To
		}
	}
	switch len(pairs) {
	case 1:
		return first, true
	case 2:
		if pairs[0].from == pairs[1].to && pairs[0].to == pairs[1].from {
			return first, true
		}
	}
	return "", false
}
