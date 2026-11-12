package enrich

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSteps(t *testing.T) {
	cases := []struct {
		in      string
		want    Steps
		wantErr bool
	}{
		{"all", Steps{Platforms: true, Routes: true, Wheelchair: true, PlatformCodes: true, OSMRoutes: true, Entrances: true}, false},
		{"steig,routes", Steps{Platforms: true, Routes: true}, false},
		{"wheelchair", Steps{Wheelchair: true}, false},
		{"platformcodes,osmroutes,entrances", Steps{PlatformCodes: true, OSMRoutes: true, Entrances: true}, false},
		{"", Steps{}, false},
		{"foo", Steps{}, true},
	}
	for _, c := range cases {
		got, err := ParseSteps(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("ParseSteps(%q) err = %v, wantErr %v", c.in, err, c.wantErr)
			continue
		}
		if err == nil && got != c.want {
			t.Errorf("ParseSteps(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestReportExampleCaps(t *testing.T) {
	var r Report
	for i := 0; i < 25; i++ {
		r.Example("example %d", i)
	}
	if len(r.Examples) != 20 {
		t.Errorf("len(Examples) = %d, want 20", len(r.Examples))
	}
	for i := 0; i < 60; i++ {
		r.Conflict("conflict %d", i)
	}
	if len(r.Conflicts) != 50 {
		t.Errorf("len(Conflicts) = %d, want 50", len(r.Conflicts))
	}
}

func TestWriteReports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	in := []Report{{Step: "steig", Changed: 3}, {Step: "routes", Changed: 7}}
	if err := WriteReports(path, in); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []Report
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].Step != "steig" || out[0].Changed != 3 || out[1].Step != "routes" {
		t.Errorf("round trip mismatch: %+v", out)
	}

	if err := WriteReports("", in); err != nil {
		t.Errorf("empty path should be a no-op, got %v", err)
	}
}

func TestNeedsOSM(t *testing.T) {
	for name, c := range map[string]struct {
		s    Steps
		want bool
	}{
		"none":          {Steps{}, false},
		"rule based":    {Steps{Platforms: true, Routes: true}, false},
		"wheelchair":    {Steps{Wheelchair: true}, true},
		"platformcodes": {Steps{PlatformCodes: true}, true},
		"osmroutes":     {Steps{OSMRoutes: true}, true},
		"entrances":     {Steps{Entrances: true}, true},
	} {
		if got := c.s.NeedsOSM(); got != c.want {
			t.Errorf("%s: NeedsOSM = %v, want %v", name, got, c.want)
		}
	}
}
