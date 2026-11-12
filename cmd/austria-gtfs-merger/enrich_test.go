package main

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/enrich"
	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/patrickbr/gtfsparser"
	gtfs "github.com/patrickbr/gtfsparser/gtfs"
)

// The main feed must never contain OSM-derived data: every OSM step is only
// allowed together with a separate output file.
func TestValidateEnrichFlags(t *testing.T) {
	rule := enrich.Steps{Platforms: true, Routes: true}
	for name, c := range map[string]struct {
		steps   enrich.Steps
		out     string
		wantErr bool
	}{
		"no OSM step":                  {rule, "", false},
		"wheelchair with output":       {enrich.Steps{Wheelchair: true}, "osm.zip", false},
		"wheelchair without output":    {enrich.Steps{Wheelchair: true}, "", true},
		"platformcodes without output": {enrich.Steps{PlatformCodes: true}, "", true},
		"osmroutes without output":     {enrich.Steps{OSMRoutes: true}, "", true},
		"entrances without output":     {enrich.Steps{Entrances: true}, "", true},
		"entrances with output":        {enrich.Steps{Entrances: true}, "osm.zip", false},
		"output without OSM step":      {rule, "osm.zip", true},
		"nothing at all":               {enrich.Steps{}, "", false},
		"all steps with output":        {enrich.Steps{Platforms: true, Routes: true, Wheelchair: true, PlatformCodes: true, OSMRoutes: true, Entrances: true}, "osm.zip", false},
		"all steps without an output":  {enrich.Steps{Platforms: true, Routes: true, Wheelchair: true, PlatformCodes: true, OSMRoutes: true, Entrances: true}, "", true},
	} {
		err := validateEnrichFlags(c.steps, c.out)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", name, err, c.wantErr)
		}
	}
}

func busFeed() (*gtfsparser.Feed, *gtfs.Stop) {
	feed := gtfsparser.NewFeed()
	s := &gtfs.Stop{Id: "s", Name: "Hbf", Lat: 47.0, Lon: 13.0}
	feed.Stops["s"] = s
	r := &gtfs.Route{Id: "r", Type: 3}
	feed.Routes["r"] = r
	var st gtfs.StopTime
	st.SetStop(s)
	feed.Trips["t"] = &gtfs.Trip{Id: "t", Route: r, StopTimes: gtfs.StopTimes{st}}
	return feed, s
}

func TestRunOSMOldFormat(t *testing.T) {
	old := filepath.Join(t.TempDir(), "old.json.gz")
	f, _ := os.Create(old)
	zw := gzip.NewWriter(f)
	zw.Write([]byte(`[{"lat":47,"lon":13,"kind":"bus","wheelchair":"yes"}]`))
	zw.Close()
	f.Close()

	feed, s := busFeed()
	reports, used := runOSM(feed, enrich.Steps{Wheelchair: true}, osmInputs{FeaturesPath: old}, nil)
	if used || s.Wheelchair_boarding != 0 {
		t.Errorf("old format accepted: used=%v wheelchair=%d", used, s.Wheelchair_boarding)
	}
	if len(reports) != 1 || reports[0].Skipped == "" {
		t.Errorf("reports = %+v, want one skipped report", reports)
	}
}

func TestRunOSM(t *testing.T) {
	dir := t.TempDir()
	featPath := filepath.Join(dir, "f.json.gz")
	if err := osmfeat.Write(featPath, []osmfeat.Feature{
		{ID: 1, Lat: 47.0, Lon: 13.0, Kind: "bus", Wheelchair: "yes", Name: "Hbf", Platform: "B", Tactile: "yes"},
	}); err != nil {
		t.Fatal(err)
	}
	side := filepath.Join(dir, "side")

	feed, s := busFeed()
	reports, used := runOSM(feed, enrich.Steps{Wheelchair: true, PlatformCodes: true, OSMRoutes: true},
		osmInputs{FeaturesPath: featPath, SidecarDir: side}, nil) // no routes file: only that step is skipped
	if !used {
		t.Fatalf("OSM data not used: %+v", reports)
	}
	if s.Wheelchair_boarding != 1 || s.Platform_code != "B" {
		t.Errorf("stop = wheelchair %d platform %q, want 1 and B", s.Wheelchair_boarding, s.Platform_code)
	}
	skipped := map[string]string{}
	for _, r := range reports {
		skipped[r.Step] = r.Skipped
	}
	if skipped["osmroutes"] == "" || skipped["wheelchair"] != "" || skipped["platformcodes"] != "" {
		t.Errorf("skipped = %v, want only osmroutes skipped", skipped)
	}
	for _, name := range []string{"osm-stop-attributes.csv.gz", "osm-qa-report.json"} {
		if _, err := os.Stat(filepath.Join(side, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
}
