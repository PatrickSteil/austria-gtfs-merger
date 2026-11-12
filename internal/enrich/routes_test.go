package enrich

import (
	"fmt"
	"testing"

	"github.com/patrickbr/gtfsparser"
	gtfs "github.com/patrickbr/gtfsparser/gtfs"
)

func TestCategoryOf(t *testing.T) {
	cases := []struct {
		in   string
		want Category
		ok   bool
	}{
		{"RJX 662", "RJX", true},
		{"S 8914", "S", true},
		{"EN 40465", "EN", true},
		{"REX 1824", "REX", true},
		{"SB 10", "SB", true},
		{"1234", "", false},
		{"", "", false},
		{"  ", "", false},
	}
	for _, c := range cases {
		got, ok := CategoryOf(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("CategoryOf(%q) = %q,%v want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestExtendedType(t *testing.T) {
	cases := []struct {
		c    Category
		base int16
		want int16
	}{
		{"RJX", 2, 101}, {"RJ", 2, 101}, {"ICE", 2, 101},
		{"IC", 2, 102}, {"EC", 2, 102}, {"D", 2, 102}, {"WB", 2, 102},
		{"IR", 2, 103},
		{"NJ", 2, 105}, {"EN", 2, 105},
		{"R", 2, 106}, {"REX", 2, 106}, {"CJX", 2, 106}, {"SLB", 2, 106}, {"OS", 2, 106},
		{"S", 2, 109},
		{"SB", 7, 6}, {"LIF", 7, 6}, {"FUN", 7, 7},
		{"XYZ", 2, 2},
		{"REX", 3, 3},
	}
	for _, c := range cases {
		if got := ExtendedType(c.c, c.base); got != c.want {
			t.Errorf("ExtendedType(%q,%d) = %d, want %d", c.c, c.base, got, c.want)
		}
	}
}

func TestTextColorFor(t *testing.T) {
	if got := TextColorFor("FFFFFF"); got != "000000" {
		t.Errorf("TextColorFor(FFFFFF) = %s", got)
	}
	if got := TextColorFor("000000"); got != "FFFFFF" {
		t.Errorf("TextColorFor(000000) = %s", got)
	}
	if got := TextColorFor("zz"); got != "000000" {
		t.Errorf("invalid input should fall back to 000000, got %s", got)
	}
}

func TestIsReplacementBus(t *testing.T) {
	for _, c := range []struct {
		short, long string
		want        bool
	}{
		{"SEV", "", true},
		{"BUS SEV", "", true},
		{"X", "Schienenersatzverkehr Wien - Graz", true},
		{"SV12", "", false},
		{"SEVERIN", "", false},
		{"101", "Linz - Wels", false},
	} {
		r := &gtfs.Route{Short_name: c.short, Long_name: c.long}
		if got := isReplacementBus(r); got != c.want {
			t.Errorf("isReplacementBus(%q,%q) = %v, want %v", c.short, c.long, got, c.want)
		}
	}
}

var tripSeq int

func addRoute(feed *gtfsparser.Feed, id, short string, typ int16) *gtfs.Route {
	r := &gtfs.Route{Id: id, Short_name: short, Type: typ}
	feed.Routes[id] = r
	return r
}

func addTrip(feed *gtfsparser.Feed, r *gtfs.Route, shortName *string) {
	tripSeq++
	feed.Trips[fmt.Sprintf("t%d", tripSeq)] = &gtfs.Trip{Id: fmt.Sprintf("t%d", tripSeq), Route: r, Short_name: shortName}
}

func sp(s string) *string { return &s }

func TestRoutes(t *testing.T) {
	feed := gtfsparser.NewFeed()

	a := addRoute(feed, "A", "A1", 2)
	addTrip(feed, a, sp("RJX 662"))
	addTrip(feed, a, sp("RJX 664"))
	addTrip(feed, a, sp("RJ 70"))

	b := addRoute(feed, "B", "B1", 2)
	addTrip(feed, b, sp("S 100"))
	addTrip(feed, b, sp("REX 200"))

	c := addRoute(feed, "C", "SEV", 3)

	d := addRoute(feed, "D", "D1", 2)
	addTrip(feed, d, nil)

	e := addRoute(feed, "E", "E1", 3)

	f := addRoute(feed, "F", "F1", 2)
	f.Color, f.Text_color = "FF0000", "FFFFFF"
	addTrip(feed, f, sp("IC 1"))

	g := addRoute(feed, "G", "G1", 2)
	g.Long_name = "????? - ?????"
	addTrip(feed, g, sp("R 1"))

	rep := Routes(feed, RouteOptions{ExtendedTypes: true})

	if a.Type != 101 || a.Desc != "Railjet Xpress" {
		t.Errorf("A = type %d desc %q", a.Type, a.Desc)
	}
	if want, _ := ColorFor("RJX"); a.Color != want || a.Text_color != TextColorFor(want) {
		t.Errorf("A colors = %q/%q", a.Color, a.Text_color)
	}
	if b.Type != 106 {
		t.Errorf("B tie should pick lower HVT type 106, got %d", b.Type)
	}
	if rep.Counts["mixed_routes"] != 2 {
		t.Errorf("mixed_routes = %d, want 2", rep.Counts["mixed_routes"])
	}
	if c.Type != 714 {
		t.Errorf("C (SEV) type = %d, want 714", c.Type)
	}
	if d.Type != 2 || d.Desc != "" {
		t.Errorf("D (no category) changed: type %d desc %q", d.Type, d.Desc)
	}
	if e.Type != 3 {
		t.Errorf("E (no trips) changed: %d", e.Type)
	}
	if f.Color != "FF0000" || f.Text_color != "FFFFFF" {
		t.Errorf("F colors overwritten: %q/%q", f.Color, f.Text_color)
	}
	if g.Long_name != "" {
		t.Errorf("G garbage long name not cleared: %q", g.Long_name)
	}
	if rep.Step != "routes" || rep.Changed == 0 {
		t.Errorf("report = %+v", rep)
	}
}

func TestRoutesWithoutExtendedTypes(t *testing.T) {
	feed := gtfsparser.NewFeed()
	a := addRoute(feed, "A", "A1", 2)
	addTrip(feed, a, sp("RJX 662"))
	c := addRoute(feed, "C", "SEV", 3)

	Routes(feed, RouteOptions{ExtendedTypes: false})

	if a.Type != 2 || c.Type != 3 {
		t.Errorf("types changed without opt-in: %d %d", a.Type, c.Type)
	}
	if a.Desc != "Railjet Xpress" {
		t.Errorf("route_desc should still be filled, got %q", a.Desc)
	}
}
