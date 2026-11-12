package enrich

import (
	"bufio"
	"compress/gzip"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/patrickbr/gtfsparser"
)

func extra(kind string, dy float64) osmfeat.Feature {
	return osmfeat.Feature{ID: int64(dy) + 1, Lat: baseLat + dy*metersLat, Lon: baseLon, Kind: kind}
}

func TestSidecarRows(t *testing.T) {
	feed := gtfsparser.NewFeed()
	wcStop(feed, "b", "B", 3, 1000) // no OSM match: must be absent
	wcStop(feed, "a", "A", 3, 0)

	platform := feat(5, "bus", "", "A", "")
	platform.Tactile, platform.Bench = "yes", "no"
	feats := []osmfeat.Feature{
		platform,
		extra("bicycle_parking", 20), extra("bicycle_parking", 90), extra("bicycle_parking", 150),
		extra("elevator", 50), extra("elevator", 150),
	}
	rows := SidecarRows(Match(feed, feats, DefaultWheelchairOptions()), feats)

	want := []SidecarRow{{StopID: "a", Tactile: "yes", Bench: "no", Shelter: "", BicycleParking100m: 2, Elevators100m: 1}}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows = %+v, want %+v", rows, want)
	}
}

func TestWriteSidecar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.csv.gz")
	err := WriteSidecar(path, []SidecarRow{{StopID: "a", Tactile: "yes", Bench: "no", BicycleParking100m: 2, Elevators100m: 1}})
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(path)
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(zr)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	want := []string{"stop_id,tactile_paving,bench,shelter,bicycle_parking_100m,elevators_100m", "a,yes,no,,2,1"}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %q, want %q", lines, want)
	}
}
