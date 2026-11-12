package enrich

import (
	"regexp"
	"sort"
	"strings"

	"github.com/patrickbr/gtfsparser"
	gtfs "github.com/patrickbr/gtfsparser/gtfs"
)

var (
	replacementRe = regexp.MustCompile(`(?i)\b(sev|schienenersatz\w*)\b`)
	questionRe    = regexp.MustCompile(`^\?+( - \?+)*$`)
)

// isReplacementBus reports whether a bus route is a rail replacement service,
// judging by its names.
func isReplacementBus(r *gtfs.Route) bool {
	return replacementRe.MatchString(r.Short_name) || replacementRe.MatchString(r.Long_name)
}

// RouteOptions controls the routes step.
type RouteOptions struct {
	// ExtendedTypes enables writing extended (HVT) route_type values.
	ExtendedTypes bool
}

// Routes derives route metadata from the product category of each route's
// trips: route_desc, route_color/route_text_color (only when both are empty)
// and, if enabled, extended route_type values. The dominant category (most
// trips) decides; ties go to the lower extended type. Routes are never split.
func Routes(feed *gtfsparser.Feed, opts RouteOptions) Report {
	rep := Report{Step: "routes"}

	cats := make(map[*gtfs.Route]map[Category]int)
	for _, t := range feed.Trips {
		if t.Route == nil || t.Short_name == nil {
			continue
		}
		c, ok := CategoryOf(*t.Short_name)
		if !ok {
			continue
		}
		if cats[t.Route] == nil {
			cats[t.Route] = make(map[Category]int)
		}
		cats[t.Route][c]++
	}

	ids := make([]string, 0, len(feed.Routes))
	for id := range feed.Routes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		r := feed.Routes[id]
		changed := false

		if r.Long_name != "" && questionRe.MatchString(r.Long_name) {
			r.Long_name = ""
			rep.Count("cleared_long_names", 1)
			changed = true
		}

		base := r.Type
		if opts.ExtendedTypes && base == 3 && isReplacementBus(r) {
			r.Type = 714
			rep.Count("extended_types", 1)
			changed = true
		}

		counts := cats[r]
		if len(counts) > 0 {
			winner := dominantCategory(counts, base)
			if len(counts) > 1 {
				rep.Count("mixed_routes", 1)
				rep.Example("mixed route %s: %v -> %s", r.Id, counts, winner)
			}
			if opts.ExtendedTypes {
				if t := ExtendedType(winner, base); t != base {
					r.Type = t
					rep.Count("extended_types", 1)
					changed = true
				}
			}
			if r.Desc == "" {
				r.Desc = DisplayName(winner)
				rep.Count("described", 1)
				changed = true
			}
			if colorUnset(r) {
				if col, ok := ColorFor(winner); ok {
					r.Color, r.Text_color = col, TextColorFor(col)
					if rep.PaletteRoutes == nil {
						rep.PaletteRoutes = make(map[string]bool)
					}
					rep.PaletteRoutes[r.Id] = true
					rep.Count("colored", 1)
					changed = true
				}
			}
		}

		if changed {
			rep.Changed++
		}
	}
	return rep
}

// dominantCategory picks the category with the most trips. Ties go to the
// lower extended type, then to the lexically smaller category.
func dominantCategory(counts map[Category]int, base int16) Category {
	var best Category
	first := true
	for c, n := range counts {
		if first {
			best, first = c, false
			continue
		}
		bn := counts[best]
		switch {
		case n > bn:
			best = c
		case n == bn:
			ct, bt := ExtendedType(c, base), ExtendedType(best, base)
			if ct < bt || (ct == bt && c < best) {
				best = c
			}
		}
	}
	return best
}

// colorUnset reports whether a route has no colors of its own. The GTFS
// parser fills missing colors with white background and black text, which the
// writer omits again, so those defaults count as unset too.
func colorUnset(r *gtfs.Route) bool {
	bgUnset := r.Color == "" || strings.EqualFold(r.Color, "FFFFFF")
	fgUnset := r.Text_color == "" || strings.EqualFold(r.Text_color, "000000")
	return bgUnset && fgUnset
}
