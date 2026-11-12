package osmfeat

import (
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
		{map[string]string{"railway": "subway_entrance"}, "subway", true},
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
