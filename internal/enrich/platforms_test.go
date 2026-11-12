package enrich

import (
	"testing"

	"github.com/patrickbr/gtfsparser"
	gtfs "github.com/patrickbr/gtfsparser/gtfs"
)

func TestIsPlatformName(t *testing.T) {
	for in, want := range map[string]bool{
		"1": true, "1,2": true, "10A": true, "13A-69A": true, "G1": true, "G12": true,
		"Wien Hbf": false, "Bahnhof 1": false, "": false, "U1": false,
	} {
		if got := isPlatformName(in); got != want {
			t.Errorf("isPlatformName(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsModeWord(t *testing.T) {
	for in, want := range map[string]bool{
		"Bus": true, "sev": true, "Zug": true, "Busbahnhof": false, "Wien": false,
	} {
		if got := isModeWord(in); got != want {
			t.Errorf("isModeWord(%q) = %v, want %v", in, got, want)
		}
	}
}

func addStop(feed *gtfsparser.Feed, id, name string, parent *gtfs.Stop, locType int8, platformCode string) *gtfs.Stop {
	s := &gtfs.Stop{Id: id, Name: name, Parent_station: parent, Location_type: locType, Platform_code: platformCode}
	feed.Stops[id] = s
	return s
}

func TestPlatforms(t *testing.T) {
	feed := gtfsparser.NewFeed()
	p1 := addStop(feed, "Pat:1:1", "Wien Gersthof", nil, 1, "")
	c1 := addStop(feed, "at:1:1:0:1", "10A", p1, 0, "")
	c2 := addStop(feed, "at:1:1:0:2", "Bus", p1, 0, "")
	c3 := addStop(feed, "at:1:1:0:3", "Wien Gersthof", p1, 0, "")
	c4 := addStop(feed, "at:1:1:0:4", "12", p1, 0, "5")
	// A synthetic parent carries the platform name of its first child.
	p2 := addStop(feed, "Pat:2:2", "3", nil, 1, "")
	c5 := addStop(feed, "at:2:2:0:3", "3", p2, 0, "")

	rep := Platforms(feed)

	if c1.Name != "Wien Gersthof" || c1.Platform_code != "10A" {
		t.Errorf("c1 = %q/%q, want Wien Gersthof/10A", c1.Name, c1.Platform_code)
	}
	if c2.Name != "Wien Gersthof" || c2.Platform_code != "" {
		t.Errorf("c2 = %q/%q, want Wien Gersthof/empty", c2.Name, c2.Platform_code)
	}
	if c3.Name != "Wien Gersthof" || c3.Platform_code != "" {
		t.Errorf("c3 changed: %q/%q", c3.Name, c3.Platform_code)
	}
	if c4.Name != "Wien Gersthof" || c4.Platform_code != "5" {
		t.Errorf("c4 = %q/%q, want Wien Gersthof/5", c4.Name, c4.Platform_code)
	}
	if len(rep.Conflicts) != 1 {
		t.Errorf("conflicts = %v, want exactly 1", rep.Conflicts)
	}
	if c5.Name != "3" {
		t.Errorf("c5 under synthetic parent was renamed to %q", c5.Name)
	}
	if rep.Changed != 3 {
		t.Errorf("Changed = %d, want 3", rep.Changed)
	}
	if rep.Step != "steig" {
		t.Errorf("Step = %q", rep.Step)
	}
}
