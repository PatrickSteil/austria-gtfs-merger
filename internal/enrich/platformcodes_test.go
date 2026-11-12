package enrich

import (
	"testing"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/patrickbr/gtfsparser"
)

func platformFeat(dy float64, platform string) osmfeat.Feature {
	f := feat(dy, "rail", "", "", "")
	f.Platform = platform
	return f
}

func TestPlatformCodes(t *testing.T) {
	feed := gtfsparser.NewFeed()
	near := wcStop(feed, "near", "A", 2, 0)
	far := wcStop(feed, "far", "B", 2, 1000)
	has := wcStop(feed, "has", "C", 2, 2000)
	has.Platform_code = "5"
	tied := wcStop(feed, "tied", "D", 2, 3000)
	none := wcStop(feed, "none", "E", 2, 4000)
	same := wcStop(feed, "same", "F", 2, 5000)
	same.Platform_code = "7"

	feats := []osmfeat.Feature{
		platformFeat(10, "3"),
		platformFeat(1000+20, "3"),
		platformFeat(2000+5, "3"),
		platformFeat(3000+10, "3"), platformFeat(3000-10, "4"),
		platformFeat(4000+5, ""),
		platformFeat(5000+5, "7"),
	}
	rep := PlatformCodes(Match(feed, feats, DefaultWheelchairOptions()))

	for _, c := range []struct {
		name string
		got  string
		want string
	}{
		{"near", near.Platform_code, "3"},
		{"far (20 m)", far.Platform_code, ""},
		{"has (source kept)", has.Platform_code, "5"},
		{"tied (3 vs 4)", tied.Platform_code, ""},
		{"none (no platform tag)", none.Platform_code, ""},
		{"same (agrees)", same.Platform_code, "7"},
	} {
		if c.got != c.want {
			t.Errorf("%s: platform_code = %q, want %q", c.name, c.got, c.want)
		}
	}
	if len(rep.Conflicts) != 2 {
		t.Errorf("conflicts = %v, want 2 (has, tied)", rep.Conflicts)
	}
	if rep.Step != "platformcodes" || rep.Changed != 1 {
		t.Errorf("report = %+v, want Step platformcodes and Changed 1", rep)
	}
}
