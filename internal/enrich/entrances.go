package enrich

import (
	"fmt"
	"math"
	"sort"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/patrickbr/gtfsparser"
	gtfs "github.com/patrickbr/gtfsparser/gtfs"
)

const (
	// entranceRadiusM is how close an OSM entrance must be to a station, and
	// how far from an existing feed entrance it must be to count as new.
	entranceRadiusM = 80.0
	// walkOverheadS and walkSpeedMS give the estimated traversal time.
	walkOverheadS = 30
	walkSpeedMS   = 1.0
)

// Entrances adds OSM station entrances that the feed does not have, as
// location_type=2 stops below the nearest suitable station, each linked by
// walkway pathways to every boarding stop of the station (so no platform of a
// station with pathways is left unreachable).
//
// A station is suitable if it has boarding stops served by rail, subway or
// tram and no entrance (location_type 2 child) yet. An OSM entrance is
// skipped if any existing feed entrance lies within entranceRadiusM. Pathways
// cannot end at a station in GTFS, hence the links to boarding stops; their
// length is the straight-line distance and their traversal time an estimate.
func Entrances(feed *gtfsparser.Feed, feats []osmfeat.Feature) Report {
	rep := Report{Step: "entrances"}

	var existing []*gtfs.Stop
	hasEntrance := make(map[string]bool)
	boarding := make(map[string][]*gtfs.Stop)
	kinds := stopKinds(feed)

	ids := make([]string, 0, len(feed.Stops))
	for id := range feed.Stops {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		s := feed.Stops[id]
		if s.Parent_station == nil {
			continue
		}
		switch s.Location_type {
		case 2:
			existing = append(existing, s)
			hasEntrance[s.Parent_station.Id] = true
		case 0:
			boarding[s.Parent_station.Id] = append(boarding[s.Parent_station.Id], s)
		}
	}
	// Stations are suitable by their boarding stops.
	var stations []*gtfs.Stop
	for _, id := range ids {
		st := feed.Stops[id]
		if st.Location_type != 1 || !st.HasLatLon() || hasEntrance[id] || len(boarding[id]) == 0 {
			continue
		}
		railLike := false
		for _, b := range boarding[id] {
			if kinds[b.Id]["rail"] || kinds[b.Id]["subway"] || kinds[b.Id]["tram"] {
				railLike = true
			}
		}
		if railLike {
			stations = append(stations, st)
		}
	}

	var ents []osmfeat.Feature
	for _, f := range feats {
		if f.Kind == "entrance" {
			ents = append(ents, f)
		}
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].ID < ents[j].ID })

	for _, f := range ents {
		eid := fmt.Sprintf("osm:entrance:%d", f.ID)
		if _, dup := feed.Stops[eid]; dup {
			continue
		}
		near := false
		for _, e := range existing {
			if e.HasLatLon() && distM(f.Lat, f.Lon, float64(e.Lat), float64(e.Lon)) <= entranceRadiusM {
				near = true
				break
			}
		}
		if near {
			continue
		}
		var st *gtfs.Stop
		best := entranceRadiusM
		for _, c := range stations {
			if d := distM(f.Lat, f.Lon, float64(c.Lat), float64(c.Lon)); d <= best {
				st, best = c, d
			}
		}
		if st == nil {
			continue
		}

		name := st.Name + " Eingang"
		if f.Ref != "" {
			name += " " + f.Ref
		}
		e := &gtfs.Stop{
			Id: eid, Name: name, Lat: float32(f.Lat), Lon: float32(f.Lon),
			Location_type: 2, Parent_station: st, Wheelchair_boarding: wheelchairValue(f.Wheelchair),
		}
		feed.Stops[eid] = e

		bs := append([]*gtfs.Stop(nil), boarding[st.Id]...)
		sort.Slice(bs, func(i, j int) bool {
			di := distM(f.Lat, f.Lon, float64(bs[i].Lat), float64(bs[i].Lon))
			dj := distM(f.Lat, f.Lon, float64(bs[j].Lat), float64(bs[j].Lon))
			if di != dj {
				return di < dj
			}
			return bs[i].Id < bs[j].Id
		})
		for _, b := range bs {
			d := distM(f.Lat, f.Lon, float64(b.Lat), float64(b.Lon))
			pid := fmt.Sprintf("osm:pw:%d:%s", f.ID, b.Id)
			feed.Pathways[pid] = &gtfs.Pathway{
				Id: pid, From_stop: e, To_stop: b, Mode: 1, Is_bidirectional: true,
				Length: float32(d), Traversal_time: walkOverheadS + int(math.Round(d/walkSpeedMS)),
			}
			rep.Count("pathways", 1)
		}
		rep.Changed++
		rep.Count("entrances", 1)
		rep.Example("%s: %s at %.0f m from %s, %d pathway(s)", eid, name, best, st.Id, len(bs))
	}
	return rep
}
