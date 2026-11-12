package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/enrich"
	"github.com/patrickbr/gtfsparser"
)

// Stats summarises a merged feed. It is persisted between runs so the next
// run can detect a sudden drop in content.
type Stats struct {
	Agencies   int `json:"agencies"`
	Stops      int `json:"stops"`
	Routes     int `json:"routes"`
	Trips      int `json:"trips"`
	RailRoutes int `json:"rail_routes"`
	// WheelchairKnown is the number of stops with wheelchair_boarding != 0.
	WheelchairKnown int `json:"wheelchair_known"`
	// ExtendedRouteTypes is the number of routes with an extended (>= 100) type.
	ExtendedRouteTypes int `json:"extended_route_types"`
}

// Limits bounds what a plausible merged feed looks like. A zero value
// disables the corresponding check.
type Limits struct {
	MinAgencies   int
	MinTrips      int
	MinRailRoutes int
	// MaxDropPct is the largest tolerated decrease, in percent, of any
	// count relative to the previous run's stats.
	MaxDropPct int
}

// computeStats counts the entities of a parsed feed. Rail routes are the
// basic GTFS type 2 and the extended 100-199 (railway service) range.
func computeStats(feed *gtfsparser.Feed) Stats {
	s := Stats{
		Agencies: len(feed.Agencies),
		Stops:    len(feed.Stops),
		Routes:   len(feed.Routes),
		Trips:    len(feed.Trips),
	}
	for _, r := range feed.Routes {
		if r.Type == 2 || (r.Type >= 100 && r.Type < 200) {
			s.RailRoutes++
		}
		if r.Type >= 100 {
			s.ExtendedRouteTypes++
		}
	}
	s.WheelchairKnown = enrich.WheelchairKnown(feed)
	return s
}

// checkStats returns a description of every violated expectation. prev may
// be nil when there is no previous run to compare against.
func checkStats(cur Stats, prev *Stats, lim Limits) []string {
	var problems []string

	atLeast := func(name string, got, min int) {
		if min > 0 && got < min {
			problems = append(problems, fmt.Sprintf("%s: %d is below the minimum of %d", name, got, min))
		}
	}
	atLeast("agencies", cur.Agencies, lim.MinAgencies)
	atLeast("trips", cur.Trips, lim.MinTrips)
	atLeast("rail routes", cur.RailRoutes, lim.MinRailRoutes)

	if prev != nil && lim.MaxDropPct > 0 {
		dropped := func(name string, got, was int) {
			// got < was*(100-pct)/100, in integers
			if was > 0 && got*100 < was*(100-lim.MaxDropPct) {
				problems = append(problems, fmt.Sprintf("%s: %d is more than %d%% below the previous run (%d)",
					name, got, lim.MaxDropPct, was))
			}
		}
		dropped("agencies", cur.Agencies, prev.Agencies)
		dropped("stops", cur.Stops, prev.Stops)
		dropped("routes", cur.Routes, prev.Routes)
		dropped("trips", cur.Trips, prev.Trips)
		dropped("rail routes", cur.RailRoutes, prev.RailRoutes)
	}
	return problems
}

// loadStats reads previously saved stats; it returns nil if there are none.
func loadStats(path string) (*Stats, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s Stats
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &s, nil
}

// saveStats writes the stats atomically.
func saveStats(path string, s Stats) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// warnings returns non-blocking notices about the enrichment: the OSM step was
// skipped although the previous run had wheelchair data, or the number of
// stops with wheelchair data fell by more than 20 %. They never stop a release.
func warnings(cur Stats, prev *Stats, wheelchairSkipped bool) []string {
	if prev == nil || prev.WheelchairKnown == 0 {
		return nil
	}
	var w []string
	if wheelchairSkipped {
		w = append(w, fmt.Sprintf("wheelchair step was skipped; the previous run had %d stops with wheelchair_boarding", prev.WheelchairKnown))
	} else if cur.WheelchairKnown*100 < prev.WheelchairKnown*80 {
		w = append(w, fmt.Sprintf("stops with wheelchair_boarding fell from %d to %d (more than 20%%)", prev.WheelchairKnown, cur.WheelchairKnown))
	}
	return w
}
