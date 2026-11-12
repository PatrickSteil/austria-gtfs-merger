package enrich

import (
	"sort"

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

// Wheelchair matches the stops to the OSM features and applies the result. See
// Match and ApplyWheelchair.
func Wheelchair(feed *gtfsparser.Feed, feats []osmfeat.Feature, opts WheelchairOptions) Report {
	return ApplyWheelchair(feed, Match(feed, feats, opts))
}

// ApplyWheelchair derives wheelchair_boarding for boarding stops from their
// OSM match and then for parent stations from their children. Source values
// are kept; when in doubt the value stays 0 (unknown).
func ApplyWheelchair(feed *gtfsparser.Feed, matches []StopMatch) Report {
	rep := Report{Step: "wheelchair"}

	for _, m := range matches {
		s := m.Stop
		if s.Wheelchair_boarding != 0 || !m.HasBest {
			continue
		}
		if m.WheelchairConflict() {
			rep.Conflict("%s (%s): conflicting OSM features nearby", s.Id, s.Name)
			continue
		}
		val := wheelchairValue(m.Best.f.Wheelchair)
		if val == 0 {
			continue
		}
		s.Wheelchair_boarding = val
		rep.Changed++
		kind := "?"
		for _, k := range []string{"bus", "rail", "tram", "subway", "ferry", "aerialway"} {
			if m.Modes[k] {
				kind = k
				break
			}
		}
		if val == 1 {
			rep.Count(kind+"_yes", 1)
		} else {
			rep.Count(kind+"_no", 1)
		}
		rep.Example("%s (%s): %d from OSM at %.0f m", s.Id, s.Name, val, m.Best.dist)
	}

	ids := make([]string, 0, len(feed.Stops))
	for id := range feed.Stops {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	deriveParents(feed, ids)
	return rep
}

// deriveParents sets wheelchair_boarding on stations without a value. Boarding
// stops with value 0 inherit their parent's value in GTFS, so a station only
// gets 1 (or 2) when every boarding stop below it is 1 (or 2); otherwise it
// stays 0 and the children keep their own values.
func deriveParents(feed *gtfsparser.Feed, ids []string) {
	type agg struct{ total, yes, no int }
	sums := make(map[string]*agg)
	for _, id := range ids {
		s := feed.Stops[id]
		if s.Location_type != 0 || s.Parent_station == nil {
			continue
		}
		a := sums[s.Parent_station.Id]
		if a == nil {
			a = &agg{}
			sums[s.Parent_station.Id] = a
		}
		a.total++
		switch s.Wheelchair_boarding {
		case 1:
			a.yes++
		case 2:
			a.no++
		}
	}
	for _, id := range ids {
		p := feed.Stops[id]
		if p.Location_type != 1 || p.Wheelchair_boarding != 0 {
			continue
		}
		a := sums[p.Id]
		switch {
		case a == nil:
		case a.yes == a.total:
			p.Wheelchair_boarding = 1
		case a.no == a.total:
			p.Wheelchair_boarding = 2
		}
	}
}
