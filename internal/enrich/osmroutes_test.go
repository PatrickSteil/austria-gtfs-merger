package enrich

import (
	"fmt"
	"testing"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/patrickbr/gtfsparser"
	gtfs "github.com/patrickbr/gtfsparser/gtfs"
)

// lineStops are five stops 500 m apart, shifted east by lonShift degrees.
var lineOffsets = []float64{0, 500, 1000, 1500, 2000}

func gtfsLine(feed *gtfsparser.Feed, id, short string, typ int16, lonShift float64, offsets []float64) *gtfs.Route {
	r := &gtfs.Route{Id: id, Short_name: short, Type: typ}
	feed.Routes[id] = r
	var sts gtfs.StopTimes
	for i, dy := range offsets {
		s := &gtfs.Stop{Id: fmt.Sprintf("%s-%d", id, i), Name: id, Lat: float32(baseLat + dy*metersLat), Lon: float32(baseLon + lonShift)}
		feed.Stops[s.Id] = s
		var st gtfs.StopTime
		st.SetStop(s)
		sts = append(sts, st)
	}
	feed.Trips["trip-"+id] = &gtfs.Trip{Id: "trip-" + id, Route: r, StopTimes: sts}
	return r
}

func relLine(id int64, mode, ref, colour, from, to string, lonShift float64, offsets []float64) osmfeat.RouteRel {
	r := osmfeat.RouteRel{ID: id, Mode: mode, Ref: ref, Colour: colour, From: from, To: to}
	for _, dy := range offsets {
		// 5 m off the GTFS stop
		r.Stops = append(r.Stops, osmfeat.Point{Lat: baseLat + (dy+5)*metersLat, Lon: baseLon + lonShift})
	}
	return r
}

func TestOSMRoutes(t *testing.T) {
	feed := gtfsparser.NewFeed()
	s1 := gtfsLine(feed, "S1", "S 1", 2, 0.00, lineOffsets)
	s2 := gtfsLine(feed, "S2", "S2", 2, 0.02, lineOffsets)
	s2.Color, s2.Text_color = "5B6770", "FFFFFF"
	s3 := gtfsLine(feed, "S3", "S3", 2, 0.04, lineOffsets)
	s3.Color, s3.Text_color = "00FF00", "000000"
	tram := gtfsLine(feed, "T1", "T1", 0, 0.06, lineOffsets)
	low := gtfsLine(feed, "L1", "L1", 3, 0.08, lineOffsets)
	mix := gtfsLine(feed, "M1", "M1", 3, 0.10, lineOffsets)
	many := gtfsLine(feed, "N1", "N1", 3, 0.12, lineOffsets)
	bare := gtfsLine(feed, "Z", "Z", 3, 0.14, lineOffsets)
	one := gtfsLine(feed, "O1", "O1", 3, 0.16, []float64{0})
	gtfsLine(feed, "Q1", "Q1", 3, 0.18, lineOffsets) // relation without stops

	rels := []osmfeat.RouteRel{
		// S1: both directions of one line
		relLine(11, "rail", "S1", "#ff0000", "Y", "X", 0.00, lineOffsets),
		relLine(10, "rail", "S1", "#ff0000", "X", "Y", 0.00, lineOffsets),
		relLine(20, "rail", "S2", "#0000ff", "A", "B", 0.02, lineOffsets),
		relLine(30, "rail", "S3", "#ff00ff", "A", "B", 0.04, lineOffsets),
		// wrong mode
		relLine(40, "bus", "T1", "#123456", "A", "B", 0.06, lineOffsets),
		// shares only 2 of 5 stops
		relLine(50, "bus", "L1", "#123456", "A", "B", 0.08, []float64{0, 500, 9000, 9500, 10000}),
		// two colours for the same line
		relLine(60, "bus", "M1", "#ff0000", "A", "B", 0.10, lineOffsets),
		relLine(61, "bus", "M1", "#00ff00", "B", "A", 0.10, lineOffsets),
		// three different from/to pairs, same colour
		relLine(70, "bus", "N1", "#ff0000", "A", "B", 0.12, lineOffsets),
		relLine(71, "bus", "N1", "#ff0000", "C", "D", 0.12, lineOffsets),
		relLine(72, "bus", "N1", "#ff0000", "E", "F", 0.12, lineOffsets),
		// relation without colour, from and to
		relLine(80, "bus", "Z", "", "", "", 0.14, lineOffsets),
		relLine(90, "bus", "O1", "#ff0000", "A", "B", 0.16, []float64{0}),
		{ID: 100, Mode: "bus", Ref: "Q1", Colour: "FF0000"},
	}
	palette := map[string]bool{"S2": true}

	rep, res := OSMRoutes(feed, rels, palette)

	if s1.Color != "FF0000" || s1.Text_color != TextColorFor("FF0000") || s1.Long_name != "X – Y" {
		t.Errorf("S1 = %q/%q/%q, want FF0000/%s/X – Y", s1.Color, s1.Text_color, s1.Long_name, TextColorFor("FF0000"))
	}
	if s2.Color != "0000FF" {
		t.Errorf("S2 palette colour not replaced: %q", s2.Color)
	}
	if s3.Color != "00FF00" || s3.Text_color != "000000" {
		t.Errorf("S3 source colour overwritten: %q/%q", s3.Color, s3.Text_color)
	}
	if tram.Color != "" || low.Color != "" || one.Color != "" {
		t.Errorf("unmatched routes changed: %q %q %q", tram.Color, low.Color, one.Color)
	}
	if mix.Color != "" || mix.Long_name != "A – B" {
		t.Errorf("M1 = colour %q name %q, want no colour (conflict) and A – B", mix.Color, mix.Long_name)
	}
	if many.Color != "FF0000" || many.Long_name != "" {
		t.Errorf("N1 = %q/%q, want colour FF0000 and no name", many.Color, many.Long_name)
	}
	if bare.Color != "" || bare.Long_name != "" {
		t.Errorf("Z changed without data: %q/%q", bare.Color, bare.Long_name)
	}
	if !contains(res.Ambiguous, "M1") {
		t.Errorf("Ambiguous = %v, want M1", res.Ambiguous)
	}
	for _, id := range []string{"S1", "S2", "S3", "N1", "Z"} {
		if !contains(res.Matched, id) {
			t.Errorf("Matched = %v, missing %s", res.Matched, id)
		}
	}
	for _, id := range []string{"T1", "L1", "O1", "Q1"} {
		if !contains(res.UnmatchedRoutes, id) {
			t.Errorf("UnmatchedRoutes = %v, missing %s", res.UnmatchedRoutes, id)
		}
	}
	if rep.Step != "osmroutes" || rep.Counts["colored"] != 3 || rep.Counts["named"] != 4 {
		t.Errorf("report = %+v, want colored 3 (S1, S2, N1) and named 4 (S1, S2, S3, M1)", rep)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
