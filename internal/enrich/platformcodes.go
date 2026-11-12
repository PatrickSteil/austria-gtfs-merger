package enrich

// platformCodeRadiusM is how close the best OSM match must be for its platform
// designation to be used as platform_code.
const platformCodeRadiusM = 15.0

// PlatformCodes fills empty platform_code values from the platform designation
// of the stop's best OSM match. A value is only used if the best match is
// within platformCodeRadiusM and no equally good candidate names a different
// platform. An existing platform_code is never replaced; a differing OSM value
// is reported as a conflict.
func PlatformCodes(matches []StopMatch) Report {
	rep := Report{Step: "platformcodes"}
	for _, m := range matches {
		if !m.HasBest || m.Best.f.Platform == "" || m.Best.dist > platformCodeRadiusM {
			continue
		}
		s, want := m.Stop, m.Best.f.Platform

		ambiguous := false
		for _, c := range m.Tied {
			if c.f.Platform != "" && c.f.Platform != want {
				ambiguous = true
			}
		}
		switch {
		case ambiguous:
			rep.Conflict("%s (%s): equally near OSM platforms disagree (%q and another)", s.Id, s.Name, want)
		case s.Platform_code == "":
			s.Platform_code = want
			rep.Changed++
			rep.Example("%s (%s): platform_code %q from OSM at %.0f m", s.Id, s.Name, want, m.Best.dist)
		case s.Platform_code != want:
			rep.Conflict("%s (%s): platform_code %q kept, OSM says %q", s.Id, s.Name, s.Platform_code, want)
		}
	}
	return rep
}
