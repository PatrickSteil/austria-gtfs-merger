package main

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/patrickbr/gtfsparser"
	gtfs "github.com/patrickbr/gtfsparser/gtfs"
)

func TestIsGarbageHeadsign(t *testing.T) {
	for s, want := range map[string]bool{
		"": true, "  ": true, "0": true, "42": true, "-": true, "...": true,
		"Wien Hbf": false, "U1": false, "Bus 2": false,
	} {
		if got := isGarbageHeadsign(s); got != want {
			t.Errorf("isGarbageHeadsign(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestParentStationID(t *testing.T) {
	for in, want := range map[string]string{
		"at:42:2121:0:6": "Pat:42:2121",
		"at:42:2121":     "Pat:42:2121",
		"simple":         "",
		"a:b":            "",
	} {
		if got := parentStationID(in); got != want {
			t.Errorf("parentStationID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnforceParentStations(t *testing.T) {
	feed := gtfsparser.NewFeed()
	add := func(id string, typ int8, lat, lon float32) *gtfs.Stop {
		s := &gtfs.Stop{Id: id, Name: "n-" + id, Lat: lat, Lon: lon, Location_type: typ}
		feed.Stops[id] = s
		return s
	}
	a := add("at:1:1:0:1", 0, 10, 20)
	b := add("at:1:1:0:2", 0, 12, 22)
	boarding := add("at:1:1:0:3", 4, 0, 0)
	clash := add("at:2:2:0:1", 0, 0, 0)
	add("Pat:2:2", 0, 0, 0) // non-station squatting on the derived ID

	if n := enforceParentStations(feed); n != 1 {
		t.Fatalf("created %d, want 1", n)
	}
	p := feed.Stops["Pat:1:1"]
	if p == nil || p.Location_type != 1 || a.Parent_station != p || b.Parent_station != p {
		t.Fatalf("children not attached to synthetic station: %+v", p)
	}
	if p.Name != "n-at:1:1:0:1" || p.Lat != 11 || p.Lon != 21 {
		t.Errorf("want first child's name and centroid (11,21), got %q (%v,%v)", p.Name, p.Lat, p.Lon)
	}
	if boarding.Parent_station != nil {
		t.Error("boarding area must not get a station parent")
	}
	if clash.Parent_station != nil {
		t.Error("non-station ID clash must not be used as parent")
	}
	if n := enforceParentStations(feed); n != 0 {
		t.Errorf("second run created %d, want 0 (idempotent)", n)
	}
}

func TestInjectFeedInfo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.zip")
	f, _ := createZip(t, path, map[string]string{"stops.txt": "a", "feed_info.txt": "old"})
	_ = f
	if err := injectFeedInfo(path, true); err != nil {
		t.Fatal(err)
	}
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got := map[string]string{}
	for _, e := range r.File {
		rc, _ := e.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		got[e.Name] = string(b)
	}
	if got["stops.txt"] != "a" || len(r.File) != 2 || got["feed_info.txt"] == "old" {
		t.Errorf("unexpected archive contents: %v", got)
	}
}

func createZip(t *testing.T, path string, files map[string]string) (string, error) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(out)
	for n, c := range files {
		fw, _ := w.Create(n)
		io.WriteString(fw, c)
	}
	w.Close()
	out.Close()
	return path, nil
}

func TestExpandNestedZips(t *testing.T) {
	dir := t.TempDir()
	plain, _ := createZip(t, filepath.Join(dir, "plain.zip"), map[string]string{"agency.txt": "x"})
	nested, _ := createZip(t, filepath.Join(dir, "nested.zip"),
		map[string]string{"a-at.zip": "1", "sub/b-at.zip": "2", "readme.txt": "r"})

	got, err := expandNestedZips([]string{plain, nested}, filepath.Join(dir, "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != plain {
		t.Fatalf("got %v", got)
	}
	for _, p := range got[1:] {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("extracted file missing: %v", err)
		}
	}
}
