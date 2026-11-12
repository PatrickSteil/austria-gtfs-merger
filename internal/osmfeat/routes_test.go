package osmfeat

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestModeOfRoute(t *testing.T) {
	for in, want := range map[string]string{
		"bus": "bus", "trolleybus": "bus", "train": "rail", "light_rail": "rail",
		"tram": "tram", "subway": "subway", "ferry": "ferry", "funicular": "aerialway",
	} {
		got, ok := ModeOfRoute(in)
		if !ok || got != want {
			t.Errorf("ModeOfRoute(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	if _, ok := ModeOfRoute("road"); ok {
		t.Error("route=road must not be a transport mode")
	}
}

func TestNormalizeColour(t *testing.T) {
	for in, want := range map[string]string{
		"#ff0000": "FF0000", "00ff00": "00FF00", "#f00": "FF0000",
		"red": "", "#GGGGGG": "", "": "", "#12345": "",
	} {
		if got := NormalizeColour(in); got != want {
			t.Errorf("NormalizeColour(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRouteRelFromTags(t *testing.T) {
	r, ok := RouteRelFromTags(7, map[string]string{
		"type": "route", "route": "train", "ref": "S1", "from": "A", "to": "B",
		"colour": "#ff0000", "network": "VOR", "operator": "OEBB",
	})
	if !ok || r.ID != 7 || r.Mode != "rail" || r.Ref != "S1" || r.From != "A" || r.To != "B" ||
		r.Colour != "FF0000" || r.Network != "VOR" || r.Operator != "OEBB" {
		t.Errorf("relation = %+v, %v", r, ok)
	}
	if _, ok := RouteRelFromTags(1, map[string]string{"type": "route", "route": "road", "ref": "A1"}); ok {
		t.Error("route=road accepted")
	}
	if _, ok := RouteRelFromTags(1, map[string]string{"type": "route", "route": "bus"}); ok {
		t.Error("relation without ref and name accepted")
	}
	if _, ok := RouteRelFromTags(1, map[string]string{"type": "multipolygon", "route": "bus", "ref": "1"}); ok {
		t.Error("type != route accepted")
	}
}

func TestRoutesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json.gz")
	in := []RouteRel{
		{ID: 9, Mode: "bus", Ref: "2", Stops: []Point{{48, 14}}},
		{ID: 3, Mode: "rail", Ref: "S1", Colour: "FF0000", Stops: []Point{{47, 13}, {47.1, 13.1}}},
	}
	if err := WriteRoutes(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadRoutes(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []RouteRel{in[1], in[0]} // sorted by ID
	if !reflect.DeepEqual(out, want) {
		t.Errorf("round trip = %+v, want %+v", out, want)
	}
}
