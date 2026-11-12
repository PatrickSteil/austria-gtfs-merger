package osmfeat

import (
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestClassifyTags(t *testing.T) {
	cases := []struct {
		tags map[string]string
		want string
		ok   bool
	}{
		{map[string]string{"highway": "bus_stop"}, "bus", true},
		{map[string]string{"railway": "platform"}, "rail", true},
		{map[string]string{"railway": "platform", "tram": "yes"}, "tram", true},
		{map[string]string{"railway": "platform", "subway": "yes"}, "subway", true},
		{map[string]string{"railway": "tram_stop"}, "tram", true},
		{map[string]string{"railway": "halt"}, "rail", true},
		{map[string]string{"railway": "subway_entrance"}, "", false}, // an entrance, see ClassifyExtra
		{map[string]string{"amenity": "ferry_terminal"}, "ferry", true},
		{map[string]string{"amenity": "bus_station"}, "bus", true},
		{map[string]string{"aerialway": "station"}, "aerialway", true},
		{map[string]string{"public_transport": "platform", "bus": "yes"}, "bus", true},
		{map[string]string{"public_transport": "stop_position", "train": "yes"}, "rail", true},
		{map[string]string{"public_transport": "platform"}, "", false},
		{map[string]string{"railway": "rail"}, "", false},
		{map[string]string{}, "", false},
	}
	for _, c := range cases {
		got, ok := ClassifyTags(c.tags)
		if got != c.want || ok != c.ok {
			t.Errorf("ClassifyTags(%v) = %q,%v want %q,%v", c.tags, got, ok, c.want, c.ok)
		}
	}
}

func TestNormalizeWheelchair(t *testing.T) {
	for in, want := range map[string]string{
		"yes": "yes", "Yes ": "yes", "designated": "yes", "no": "no", "NO": "no",
		"limited": "limited", "bad": "", "": "",
	} {
		if got := NormalizeWheelchair(in); got != want {
			t.Errorf("NormalizeWheelchair(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.json.gz")
	in := []Feature{
		{Lat: 48.3, Lon: 14.2, Kind: "bus", Wheelchair: "yes", Name: "B"},
		{Lat: 48.1, Lon: 16.4, Kind: "rail", Wheelchair: "no", Name: "A", Ref: "3", Area: true},
	}
	if err := Write(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []Feature{in[1], in[0]} // sorted by Lat, Lon, Name
	if !reflect.DeepEqual(out, want) {
		t.Errorf("round trip = %+v, want %+v", out, want)
	}
	if _, err := Read(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("Read of a missing file should fail")
	}
}

func TestPlatformRef(t *testing.T) {
	cases := []struct {
		kind string
		tags map[string]string
		want string
	}{
		{"rail", map[string]string{"ref": "3"}, "3"},
		{"rail", map[string]string{"local_ref": "7a"}, "7a"},
		{"tram", map[string]string{"ref": "B"}, "B"},
		{"bus", map[string]string{"ref": "1234"}, ""},
		{"bus", map[string]string{"local_ref": "B"}, "B"},
		{"rail", map[string]string{"ref": "1;2"}, ""},
		{"rail", map[string]string{"ref": "12345"}, ""},
		{"rail", map[string]string{"ref": "Gleis 3"}, ""},
		{"rail", map[string]string{}, ""},
	}
	for _, c := range cases {
		if got := PlatformRef(c.kind, c.tags); got != c.want {
			t.Errorf("PlatformRef(%q,%v) = %q, want %q", c.kind, c.tags, got, c.want)
		}
	}
}

func TestNormalizeTri(t *testing.T) {
	for in, want := range map[string]string{
		"yes": "yes", " Yes ": "yes", "designated": "yes", "no": "no", "NO": "no",
		"limited": "", "": "", "bad": "",
	} {
		if got := NormalizeTri(in); got != want {
			t.Errorf("NormalizeTri(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClassifyExtra(t *testing.T) {
	cases := []struct {
		tags map[string]string
		want string
		ok   bool
	}{
		{map[string]string{"amenity": "bicycle_parking"}, "bicycle_parking", true},
		{map[string]string{"highway": "elevator"}, "elevator", true},
		{map[string]string{"railway": "subway_entrance"}, "entrance", true},
		{map[string]string{"entrance": "yes", "railway": "station"}, "entrance", true},
		{map[string]string{"entrance": "main", "public_transport": "station"}, "entrance", true},
		{map[string]string{"entrance": "yes"}, "", false},
		{map[string]string{"highway": "bus_stop"}, "", false},
	}
	for _, c := range cases {
		got, ok := ClassifyExtra(c.tags)
		if got != c.want || ok != c.ok {
			t.Errorf("ClassifyExtra(%v) = %q,%v want %q,%v", c.tags, got, ok, c.want, c.ok)
		}
	}
}

func TestFeaturesVersioned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.json.gz")
	in := []Feature{{ID: 42, Lat: 48.1, Lon: 16.4, Kind: "rail", Platform: "3", Tactile: "yes", Bench: "no", Shelter: "yes", StopPos: true}}
	if err := Write(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := Read(path)
	if err != nil || !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip = %+v, %v; want %+v", out, err, in)
	}

	// A file in the old format (plain JSON array) must be rejected.
	old := filepath.Join(t.TempDir(), "old.json.gz")
	f, _ := os.Create(old)
	zw := gzip.NewWriter(f)
	zw.Write([]byte(`[{"lat":1,"lon":2,"kind":"bus"}]`))
	zw.Close()
	f.Close()
	if _, err := Read(old); !errors.Is(err, ErrFormat) {
		t.Errorf("old format: err = %v, want ErrFormat", err)
	}
}

func TestFeatureFromTags(t *testing.T) {
	cases := []struct {
		name string
		tags map[string]string
		want Feature
		ok   bool
	}{
		{"subway entrance is an entrance, not a subway stop",
			map[string]string{"railway": "subway_entrance", "wheelchair": "yes"},
			Feature{ID: 9, Kind: "entrance", Wheelchair: "yes"}, true},
		{"entrance tag wins over public_transport=station",
			map[string]string{"entrance": "yes", "public_transport": "station", "subway": "yes"},
			Feature{ID: 9, Kind: "entrance"}, true},
		{"platform keeps its ref as platform",
			map[string]string{"railway": "platform", "ref": "3", "wheelchair": "no", "name": "X", "tactile_paving": "yes"},
			Feature{ID: 9, Kind: "rail", Wheelchair: "no", Name: "X", Ref: "3", Platform: "3", Tactile: "yes"}, true},
		{"station ref is not a platform",
			map[string]string{"railway": "station", "ref": "WB"},
			Feature{ID: 9, Kind: "rail", Ref: "WB"}, true},
		{"halt ref is not a platform",
			map[string]string{"railway": "halt", "ref": "AB"},
			Feature{ID: 9, Kind: "rail", Ref: "AB"}, true},
		{"stop position",
			map[string]string{"public_transport": "stop_position", "bus": "yes"},
			Feature{ID: 9, Kind: "bus", StopPos: true}, true},
		{"bus stop uses local_ref only",
			map[string]string{"highway": "bus_stop", "local_ref": "B", "ref": "1234"},
			Feature{ID: 9, Kind: "bus", Ref: "B", Platform: "B"}, true},
		{"bicycle parking", map[string]string{"amenity": "bicycle_parking"}, Feature{ID: 9, Kind: "bicycle_parking"}, true},
		{"irrelevant", map[string]string{"shop": "bakery"}, Feature{}, false},
	}
	for _, c := range cases {
		got, ok := FeatureFromTags(9, c.tags, 0, 0, false)
		if ok != c.ok || (ok && !reflect.DeepEqual(got, c.want)) {
			t.Errorf("%s: got %+v,%v want %+v,%v", c.name, got, ok, c.want, c.ok)
		}
	}
}
