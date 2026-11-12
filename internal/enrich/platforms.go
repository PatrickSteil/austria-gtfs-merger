package enrich

import (
	"regexp"
	"sort"
	"strings"

	"github.com/patrickbr/gtfsparser"
)

var (
	platformRe = regexp.MustCompile(`^[0-9]{1,3}[A-Za-z]?([,/-][0-9]{1,3}[A-Za-z]?)*$`)
	gateRe     = regexp.MustCompile(`^G[0-9]+$`)

	// modeWords are stop names that name a mode instead of a place.
	modeWords = map[string]bool{
		"bus": true, "bahn": true, "zug": true, "tram": true, "sev": true, "ubahn": true,
	}
)

// isPlatformName reports whether name looks like a platform designation
// ("1", "1,2", "10A", "13A-69A", "G1") rather than a place name.
func isPlatformName(name string) bool {
	return platformRe.MatchString(name) || gateRe.MatchString(name)
}

// isModeWord reports whether name is just a transport mode word ("Bus", "SEV").
func isModeWord(name string) bool {
	return modeWords[strings.ToLower(strings.TrimSpace(name))]
}

// Platforms replaces platform-only stop names of boarding stops with the name
// of their parent station and keeps the platform in platform_code.
//
// Only boarding stops (location_type 0) with a parent are touched. A parent
// whose own name looks like a platform is not a usable name source (it is
// typically a synthetic parent that copied its first child's name).
func Platforms(feed *gtfsparser.Feed) Report {
	rep := Report{Step: "steig"}

	ids := make([]string, 0, len(feed.Stops))
	for id := range feed.Stops {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		s := feed.Stops[id]
		p := s.Parent_station
		if s.Location_type != 0 || p == nil || p.Name == "" || s.Name == p.Name {
			continue
		}
		if isPlatformName(p.Name) || isModeWord(p.Name) {
			continue
		}

		platform, mode := isPlatformName(s.Name), isModeWord(s.Name)
		if !platform && !mode {
			rep.Count("left_alone", 1)
			continue
		}

		old := s.Name
		s.Name = p.Name
		if platform {
			switch {
			case s.Platform_code == "":
				s.Platform_code = old
			case s.Platform_code != old:
				rep.Conflict("%s: platform_code %q kept, name was %q", s.Id, s.Platform_code, old)
			}
		}
		rep.Changed++
		rep.Example("%s: %q -> %q", s.Id, old, s.Name)
	}
	return rep
}
