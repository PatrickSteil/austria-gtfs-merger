package enrich

import (
	"encoding/json"
	"os"
	"sort"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/patrickbr/gtfsparser"
)

const (
	qaMaxExamples       = 50
	stopPositionRadiusM = 100.0
	stopPositionWarn1M  = 15.0
	stopPositionWarn2M  = 30.0
	osmStopRadiusM      = 50.0
	elevatorRadiusM     = 100.0
)

type QAStop struct {
	StopID string  `json:"stop_id"`
	Name   string  `json:"name"`
	Mode   string  `json:"mode"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
}

type QAStopsSection struct {
	Total    int            `json:"total"`
	ByMode   map[string]int `json:"by_mode"`
	Examples []QAStop       `json:"examples"`
}

type QAOffset struct {
	StopID string  `json:"stop_id"`
	Name   string  `json:"name"`
	DistM  float64 `json:"distance_m"`
}

type QAStopPositions struct {
	Checked int        `json:"checked"`
	MedianM float64    `json:"median_m"`
	Over15m int        `json:"over_15m"`
	Over30m int        `json:"over_30m"`
	Largest []QAOffset `json:"largest"`
}

type QAOSM struct {
	ID   int64   `json:"osm_id"`
	Kind string  `json:"kind"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

type QAOSMSection struct {
	Total    int            `json:"total"`
	ByMode   map[string]int `json:"by_mode"`
	Examples []QAOSM        `json:"examples"`
}

type QAList struct {
	Count    int      `json:"count"`
	Examples []string `json:"examples"`
}

type QARoutes struct {
	RelationsTotal  int    `json:"relations_total"`
	Matched         QAList `json:"matched"`
	Ambiguous       QAList `json:"ambiguous"`
	UnmatchedRoutes QAList `json:"gtfs_routes_without_relation"`
	UnmatchedGroups QAList `json:"relations_without_gtfs_route"`
}

type QAElevators struct {
	Total                    int `json:"total"`
	StationsWithElevator100m int `json:"stations_with_elevator_100m"`
}

// QAReport compares the GTFS feed with OpenStreetMap. It only reports; it
// changes nothing in the feed.
type QAReport struct {
	StopsWithoutMatch QAStopsSection  `json:"stops_without_osm_match"`
	StopPositions     QAStopPositions `json:"stop_position_offsets"`
	OSMWithoutGTFS    QAOSMSection    `json:"osm_stops_without_gtfs_stop"`
	Routes            QARoutes        `json:"route_relations"`
	Elevators         QAElevators     `json:"elevators"`
}

func primaryMode(modes map[string]bool) string {
	for _, k := range []string{"bus", "rail", "tram", "subway", "ferry", "aerialway"} {
		if modes[k] {
			return k
		}
	}
	return ""
}

func qaList(ids []string) QAList {
	l := QAList{Count: len(ids), Examples: []string{}}
	for i, id := range ids {
		if i >= qaMaxExamples {
			break
		}
		l.Examples = append(l.Examples, id)
	}
	return l
}

// QA builds the report from the match result, the OSM features and the route
// pairing. Examples are capped at 50 per section.
func QA(feed *gtfsparser.Feed, feats []osmfeat.Feature, matches []StopMatch, rels []osmfeat.RouteRel, rm RouteMatchResult) QAReport {
	q := QAReport{
		StopsWithoutMatch: QAStopsSection{ByMode: map[string]int{}, Examples: []QAStop{}},
		StopPositions:     QAStopPositions{Largest: []QAOffset{}},
		OSMWithoutGTFS:    QAOSMSection{ByMode: map[string]int{}, Examples: []QAOSM{}},
	}

	// Boarding stops without an OSM counterpart.
	for _, m := range matches {
		if m.HasBest {
			continue
		}
		mode := primaryMode(m.Modes)
		q.StopsWithoutMatch.Total++
		q.StopsWithoutMatch.ByMode[mode]++
		if len(q.StopsWithoutMatch.Examples) < qaMaxExamples {
			q.StopsWithoutMatch.Examples = append(q.StopsWithoutMatch.Examples, QAStop{
				StopID: m.Stop.Id, Name: m.Stop.Name, Mode: mode, Lat: float64(m.Stop.Lat), Lon: float64(m.Stop.Lon),
			})
		}
	}

	// Distance from each boarding stop to the nearest stop_position of its mode.
	posGrid := newFeatureGrid(feats, func(f osmfeat.Feature) bool { return f.StopPos })
	var offsets []QAOffset
	for _, m := range matches {
		best := -1.0
		posGrid.within(float64(m.Stop.Lat), float64(m.Stop.Lon), stopPositionRadiusM, func(f osmfeat.Feature, d float64) {
			if m.Modes[f.Kind] && (best < 0 || d < best) {
				best = d
			}
		})
		if best >= 0 {
			offsets = append(offsets, QAOffset{StopID: m.Stop.Id, Name: m.Stop.Name, DistM: best})
		}
	}
	q.StopPositions.Checked = len(offsets)
	if len(offsets) > 0 {
		ds := make([]float64, len(offsets))
		for i, o := range offsets {
			ds[i] = o.DistM
			if o.DistM > stopPositionWarn1M {
				q.StopPositions.Over15m++
			}
			if o.DistM > stopPositionWarn2M {
				q.StopPositions.Over30m++
			}
		}
		sort.Float64s(ds)
		if n := len(ds); n%2 == 1 {
			q.StopPositions.MedianM = ds[n/2]
		} else {
			q.StopPositions.MedianM = (ds[n/2-1] + ds[n/2]) / 2
		}
		sort.Slice(offsets, func(i, j int) bool {
			if offsets[i].DistM != offsets[j].DistM {
				return offsets[i].DistM > offsets[j].DistM
			}
			return offsets[i].StopID < offsets[j].StopID
		})
		if len(offsets) > qaMaxExamples {
			offsets = offsets[:qaMaxExamples]
		}
		q.StopPositions.Largest = offsets
	}

	// OSM transport stops with no GTFS boarding stop of the same mode nearby.
	stopCells := make(map[cell][]int)
	for i, m := range matches {
		c := cellOf(float64(m.Stop.Lat), float64(m.Stop.Lon))
		stopCells[c] = append(stopCells[c], i)
	}
	for _, f := range feats {
		if f.StopPos || !transportKinds[f.Kind] {
			continue
		}
		found := false
		c0 := cellOf(f.Lat, f.Lon)
		for dy := -1; dy <= 1 && !found; dy++ {
			for dx := -1; dx <= 1 && !found; dx++ {
				for _, i := range stopCells[cell{c0.y + dy, c0.x + dx}] {
					m := matches[i]
					if m.Modes[f.Kind] && distM(f.Lat, f.Lon, float64(m.Stop.Lat), float64(m.Stop.Lon)) <= osmStopRadiusM {
						found = true
						break
					}
				}
			}
		}
		if found {
			continue
		}
		q.OSMWithoutGTFS.Total++
		q.OSMWithoutGTFS.ByMode[f.Kind]++
		if len(q.OSMWithoutGTFS.Examples) < qaMaxExamples {
			q.OSMWithoutGTFS.Examples = append(q.OSMWithoutGTFS.Examples, QAOSM{
				ID: f.ID, Kind: f.Kind, Name: f.Name, Lat: f.Lat, Lon: f.Lon,
			})
		}
	}

	// Route relations.
	q.Routes = QARoutes{
		RelationsTotal:  len(rels),
		Matched:         qaList(rm.Matched),
		Ambiguous:       qaList(rm.Ambiguous),
		UnmatchedRoutes: qaList(rm.UnmatchedRoutes),
		UnmatchedGroups: qaList(rm.UnmatchedGroups),
	}

	// Elevators are only counted (no pathway topology is built from them).
	elevGrid := newFeatureGrid(feats, func(f osmfeat.Feature) bool { return f.Kind == "elevator" })
	for _, f := range feats {
		if f.Kind == "elevator" {
			q.Elevators.Total++
		}
	}
	for _, s := range feed.Stops {
		if s.Location_type != 1 || !s.HasLatLon() {
			continue
		}
		near := false
		elevGrid.within(float64(s.Lat), float64(s.Lon), elevatorRadiusM, func(osmfeat.Feature, float64) { near = true })
		if near {
			q.Elevators.StationsWithElevator100m++
		}
	}
	return q
}

// WriteQA writes the report as indented JSON.
func WriteQA(path string, q QAReport) error {
	data, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
