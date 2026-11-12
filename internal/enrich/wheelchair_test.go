package enrich

import (
	"fmt"
	"testing"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/patrickbr/gtfsparser"
	gtfs "github.com/patrickbr/gtfsparser/gtfs"
)

const (
	baseLat = 47.0
	baseLon = 13.0
	// metersLat is the latitude offset of one meter.
	metersLat = 1.0 / 111320.0
)

// wcStop adds a boarding stop at baseLat+dyMeters and a trip of routeType that serves it.
func wcStop(feed *gtfsparser.Feed, id, name string, routeType int16, dyMeters float64) *gtfs.Stop {
	s := &gtfs.Stop{Id: id, Name: name, Lat: float32(baseLat + dyMeters*metersLat), Lon: baseLon}
	feed.Stops[id] = s
	rid := fmt.Sprintf("r-%d", routeType)
	r, ok := feed.Routes[rid]
	if !ok {
		r = &gtfs.Route{Id: rid, Type: routeType}
		feed.Routes[rid] = r
	}
	tid := "trip-" + id
	var st gtfs.StopTime
	st.SetStop(s)
	feed.Trips[tid] = &gtfs.Trip{Id: tid, Route: r, StopTimes: gtfs.StopTimes{st}}
	return s
}

func feat(dyMeters float64, kind, wheelchair, name, ref string) osmfeat.Feature {
	return osmfeat.Feature{Lat: baseLat + dyMeters*metersLat, Lon: baseLon, Kind: kind, Wheelchair: wheelchair, Name: name, Ref: ref}
}

func TestWheelchairValue(t *testing.T) {
	for in, want := range map[string]int8{"yes": 1, "no": 2, "limited": 0, "": 0} {
		if got := wheelchairValue(in); got != want {
			t.Errorf("wheelchairValue(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestStopKinds(t *testing.T) {
	feed := gtfsparser.NewFeed()
	bus := wcStop(feed, "b", "B", 3, 0)
	rail := wcStop(feed, "r", "R", 101, 0) // extended rail range
	tram := wcStop(feed, "t", "T", 0, 0)
	k := stopKinds(feed)
	if !k[bus.Id]["bus"] || !k[rail.Id]["rail"] || !k[tram.Id]["tram"] {
		t.Errorf("kinds = %v", k)
	}
	if k[bus.Id]["rail"] {
		t.Error("bus stop must not be rail")
	}
}

func TestWheelchairMatching(t *testing.T) {
	feed := gtfsparser.NewFeed()
	yes := wcStop(feed, "yes", "Hbf", 3, 0)
	no := wcStop(feed, "no", "Hbf", 3, 1000)
	far := wcStop(feed, "far", "Hbf", 3, 2000)
	conflict := wcStop(feed, "conflict", "Hbf", 3, 3000)
	wrongMode := wcStop(feed, "wrongmode", "Hbf", 3, 4000)
	limited := wcStop(feed, "limited", "Hbf", 3, 5000)
	refStop := wcStop(feed, "ref", "Gleis", 2, 6000)
	refStop.Platform_code = "7"
	noTrips := &gtfs.Stop{Id: "notrips", Name: "X", Lat: float32(baseLat + 7000*metersLat), Lon: baseLon}
	feed.Stops[noTrips.Id] = noTrips
	zero := wcStop(feed, "zero", "Z", 3, 0)
	zero.Lat, zero.Lon = 0, 0

	feats := []osmfeat.Feature{
		feat(10, "bus", "yes", "Hbf", ""),
		feat(1000+10, "bus", "no", "Hbf", ""),
		feat(2000+100, "bus", "yes", "Hbf", ""),
		feat(3000+10, "bus", "yes", "Hbf", ""),
		feat(3000-10, "bus", "no", "Hbf", ""),
		feat(4000+5, "rail", "yes", "Hbf", ""),
		feat(5000+5, "bus", "limited", "Hbf", ""),
		feat(6000+5, "rail", "no", "", "8"),
		feat(6000+15, "rail", "yes", "", "7"),
		feat(7000, "bus", "yes", "X", ""),
	}

	rep := Wheelchair(feed, feats, DefaultWheelchairOptions())

	for _, c := range []struct {
		name string
		s    *gtfs.Stop
		want int8
	}{
		{"yes", yes, 1}, {"no", no, 2}, {"far", far, 0}, {"conflict", conflict, 0},
		{"wrongmode", wrongMode, 0}, {"limited", limited, 0}, {"ref", refStop, 1},
		{"notrips", noTrips, 0}, {"zero", zero, 0},
	} {
		if c.s.Wheelchair_boarding != c.want {
			t.Errorf("%s: wheelchair_boarding = %d, want %d", c.name, c.s.Wheelchair_boarding, c.want)
		}
	}
	if len(rep.Conflicts) != 1 {
		t.Errorf("conflicts = %v, want 1", rep.Conflicts)
	}
	if rep.Step != "wheelchair" || rep.Changed != 3 {
		t.Errorf("report = %+v, want Step wheelchair and Changed 3", rep)
	}
	if got := WheelchairKnown(feed); got != 3 {
		t.Errorf("WheelchairKnown = %d, want 3", got)
	}
}

func TestWheelchairKeepsSourceValues(t *testing.T) {
	feed := gtfsparser.NewFeed()
	s := wcStop(feed, "s", "Hbf", 3, 0)
	s.Wheelchair_boarding = 2
	Wheelchair(feed, []osmfeat.Feature{feat(5, "bus", "yes", "Hbf", "")}, DefaultWheelchairOptions())
	if s.Wheelchair_boarding != 2 {
		t.Errorf("source value overwritten: %d", s.Wheelchair_boarding)
	}
}

func TestWheelchairParents(t *testing.T) {
	feed := gtfsparser.NewFeed()
	mk := func(pid string, kids ...int8) *gtfs.Stop {
		p := &gtfs.Stop{Id: pid, Name: pid, Location_type: 1, Lat: baseLat, Lon: baseLon}
		feed.Stops[pid] = p
		for i, v := range kids {
			feed.Stops[fmt.Sprintf("%s-%d", pid, i)] = &gtfs.Stop{
				Id: fmt.Sprintf("%s-%d", pid, i), Name: pid, Parent_station: p, Wheelchair_boarding: v,
				Lat: baseLat, Lon: baseLon,
			}
		}
		return p
	}
	some := mk("some", 1, 2, 0)
	allNo := mk("allno", 2, 0)
	none := mk("none", 0, 0)

	Wheelchair(feed, nil, DefaultWheelchairOptions())

	if some.Wheelchair_boarding != 1 || allNo.Wheelchair_boarding != 2 || none.Wheelchair_boarding != 0 {
		t.Errorf("parents = %d %d %d, want 1 2 0", some.Wheelchair_boarding, allNo.Wheelchair_boarding, none.Wheelchair_boarding)
	}
}
