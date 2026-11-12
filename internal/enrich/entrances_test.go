package enrich

import (
	"fmt"
	"math"
	"testing"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/patrickbr/gtfsparser"
	gtfs "github.com/patrickbr/gtfsparser/gtfs"
)

// station adds a station at baseLat+dy with n boarding stops (10 m apart,
// starting at the station) served by routes of routeType.
func station(feed *gtfsparser.Feed, id string, routeType int16, dy float64, n int) *gtfs.Stop {
	st := &gtfs.Stop{Id: id, Name: "Station " + id, Lat: float32(baseLat + dy*metersLat), Lon: baseLon, Location_type: 1}
	feed.Stops[id] = st
	for i := 0; i < n; i++ {
		b := wcStop(feed, fmt.Sprintf("%s-b%d", id, i), "Station "+id, routeType, dy+float64(i)*10)
		b.Parent_station = st
	}
	return st
}

func entranceFeat(id int64, dy float64, wheelchair string) osmfeat.Feature {
	return osmfeat.Feature{ID: id, Lat: baseLat + dy*metersLat, Lon: baseLon, Kind: "entrance", Wheelchair: wheelchair}
}

func TestEntrances(t *testing.T) {
	feed := gtfsparser.NewFeed()
	station(feed, "A", 2, 0, 2)
	station(feed, "B", 2, 2000, 6)
	c := station(feed, "C", 2, 4000, 2)
	feed.Stops["C-e"] = &gtfs.Stop{Id: "C-e", Name: "C entrance", Lat: float32(baseLat + 4010*metersLat), Lon: baseLon, Location_type: 2, Parent_station: c}
	station(feed, "D", 2, 6000, 2)
	x := station(feed, "X", 1, 6060, 1)
	feed.Stops["X-e"] = &gtfs.Stop{Id: "X-e", Name: "X entrance", Lat: float32(baseLat + 6060*metersLat), Lon: baseLon, Location_type: 2, Parent_station: x}
	station(feed, "F", 3, 8000, 2)                                                                                                    // bus only
	feed.Stops["G"] = &gtfs.Stop{Id: "G", Name: "Station G", Lat: float32(baseLat + 10000*metersLat), Lon: baseLon, Location_type: 1} // no boarding stops
	station(feed, "H", 2, 12000, 2)
	station(feed, "I1", 2, 14000, 1)
	station(feed, "I2", 2, 14050, 1)

	feats := []osmfeat.Feature{
		entranceFeat(500, 40, "yes"),                       // A
		entranceFeat(501, 2040, "no"),                      // B
		entranceFeat(502, 4040, "yes"),                     // C already has an entrance
		entranceFeat(503, 6030, "yes"),                     // D, but X's entrance is within 80 m
		entranceFeat(504, 8040, "yes"),                     // F is bus only
		entranceFeat(505, 10040, "yes"),                    // G has no boarding stops
		entranceFeat(506, 12120, "yes"),                    // H, 120 m away
		entranceFeat(507, 14010, "yes"),                    // nearest I1, I2 also in range
		{ID: 508, Lat: baseLat, Lon: baseLon, Kind: "bus"}, // not an entrance
	}

	rep := Entrances(feed, feats)

	a := feed.Stops["osm:entrance:500"]
	if a == nil || a.Location_type != 2 || a.Parent_station == nil || a.Parent_station.Id != "A" || a.Wheelchair_boarding != 1 {
		t.Fatalf("entrance 500 = %+v", a)
	}
	if a.Name != "Station A Eingang" {
		t.Errorf("name = %q", a.Name)
	}
	var pa []*gtfs.Pathway
	for _, p := range feed.Pathways {
		if p.From_stop == a {
			pa = append(pa, p)
		}
	}
	if len(pa) != 2 {
		t.Fatalf("pathways of entrance 500 = %d, want 2", len(pa))
	}
	for _, p := range pa {
		d := distM(float64(a.Lat), float64(a.Lon), float64(p.To_stop.Lat), float64(p.To_stop.Lon))
		if p.Mode != 1 || !p.Is_bidirectional || math.Abs(float64(p.Length)-d) > 1.5 ||
			p.Traversal_time != 30+int(math.Round(float64(p.Length))) {
			t.Errorf("pathway %s: mode %d bidir %v length %v time %d (distance %.1f)", p.Id, p.Mode, p.Is_bidirectional, p.Length, p.Traversal_time, d)
		}
		if p.Id != "osm:pw:500:"+p.To_stop.Id {
			t.Errorf("pathway id = %q", p.Id)
		}
	}

	b := feed.Stops["osm:entrance:501"]
	if b == nil || b.Wheelchair_boarding != 2 {
		t.Fatalf("entrance 501 = %+v", b)
	}
	nb := 0
	for _, p := range feed.Pathways {
		if p.From_stop == b {
			nb++
		}
	}
	if nb != 4 {
		t.Errorf("B pathways = %d, want 4 (the nearest of 6)", nb)
	}

	for _, id := range []string{"502", "503", "504", "505", "506", "508"} {
		if feed.Stops["osm:entrance:"+id] != nil {
			t.Errorf("entrance %s was created but should not be", id)
		}
	}
	i1 := feed.Stops["osm:entrance:507"]
	if i1 == nil || i1.Parent_station.Id != "I1" {
		t.Errorf("entrance 507 = %+v, want parent I1", i1)
	}
	if rep.Step != "entrances" || rep.Changed != 3 || rep.Counts["pathways"] != 2+4+1 {
		t.Errorf("report = %+v, want 3 entrances and 7 pathways", rep)
	}

	// A second run must not add anything.
	nStops, nPaths := len(feed.Stops), len(feed.Pathways)
	Entrances(feed, feats)
	if len(feed.Stops) != nStops || len(feed.Pathways) != nPaths {
		t.Errorf("second run changed the feed: stops %d->%d, pathways %d->%d", nStops, len(feed.Stops), nPaths, len(feed.Pathways))
	}
}
