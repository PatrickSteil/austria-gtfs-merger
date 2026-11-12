package enrich

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/patrickbr/gtfsparser"
	gtfs "github.com/patrickbr/gtfsparser/gtfs"
)

func stopPos(kind string, dy float64) osmfeat.Feature {
	f := feat(dy, kind, "", "", "")
	f.StopPos = true
	return f
}

func TestQA(t *testing.T) {
	feed := gtfsparser.NewFeed()
	wcStop(feed, "a", "A", 3, 0)
	wcStop(feed, "b", "B", 3, 1000) // no OSM counterpart
	wcStop(feed, "c", "C", 3, 2000)
	feed.Stops["S"] = &gtfs.Stop{Id: "S", Name: "Station", Lat: float32(baseLat + 7000*metersLat), Lon: baseLon, Location_type: 1}

	feats := []osmfeat.Feature{
		feat(5, "bus", "", "A", ""), stopPos("bus", 10), // a: stop position 10 m away
		feat(2000+5, "bus", "", "C", ""), stopPos("bus", 2020), // c: 20 m away
		feat(5000, "bus", "", "Nobody", ""), // OSM stop without GTFS stop
		stopPos("bus", 9000),                // stop positions alone are not "OSM stops without GTFS"
		extra("elevator", 7050), extra("elevator", 20000),
	}
	ms := Match(feed, feats, DefaultWheelchairOptions())
	rm := RouteMatchResult{Matched: []string{"r1"}, UnmatchedGroups: []string{"bus 7"}}
	for i := 0; i < 60; i++ {
		rm.UnmatchedRoutes = append(rm.UnmatchedRoutes, fmt.Sprintf("u%d", i))
	}
	q := QA(feed, feats, ms, []osmfeat.RouteRel{{ID: 1}, {ID: 2}, {ID: 3}}, rm)

	if q.StopsWithoutMatch.Total != 1 || q.StopsWithoutMatch.ByMode["bus"] != 1 || q.StopsWithoutMatch.Examples[0].StopID != "b" {
		t.Errorf("stops without match = %+v", q.StopsWithoutMatch)
	}
	sp := q.StopPositions
	if sp.Checked != 2 || sp.Over15m != 1 || sp.Over30m != 0 || sp.MedianM < 14.5 || sp.MedianM > 15.5 {
		t.Errorf("stop positions = %+v, want checked 2, over15m 1, over30m 0, median 15", sp)
	}
	if q.OSMWithoutGTFS.Total != 1 || q.OSMWithoutGTFS.Examples[0].Name != "Nobody" {
		t.Errorf("osm without gtfs = %+v", q.OSMWithoutGTFS)
	}
	if q.Elevators.Total != 2 || q.Elevators.StationsWithElevator100m != 1 {
		t.Errorf("elevators = %+v", q.Elevators)
	}
	if q.Routes.RelationsTotal != 3 || q.Routes.Matched.Count != 1 || q.Routes.UnmatchedRoutes.Count != 60 ||
		len(q.Routes.UnmatchedRoutes.Examples) != 50 || q.Routes.UnmatchedGroups.Count != 1 {
		t.Errorf("routes = %+v", q.Routes)
	}
}

func TestQAExamplesCapped(t *testing.T) {
	feed := gtfsparser.NewFeed()
	for i := 0; i < 60; i++ {
		wcStop(feed, fmt.Sprintf("s%02d", i), "S", 3, float64(i)*1000)
	}
	q := QA(feed, nil, Match(feed, nil, DefaultWheelchairOptions()), nil, RouteMatchResult{})
	if q.StopsWithoutMatch.Total != 60 || len(q.StopsWithoutMatch.Examples) != 50 {
		t.Errorf("total %d examples %d, want 60 and 50", q.StopsWithoutMatch.Total, len(q.StopsWithoutMatch.Examples))
	}
}

func TestWriteQA(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qa.json")
	if err := WriteQA(path, QAReport{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil || v["stops_without_osm_match"] == nil {
		t.Errorf("QA json invalid or missing sections: %v %s", err, data)
	}
}
