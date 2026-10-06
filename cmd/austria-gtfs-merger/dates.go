package main

import (
	"sort"
	"strconv"
	"time"

	"github.com/patrickbr/gtfsparser"
	gtfs "github.com/patrickbr/gtfsparser/gtfs"
)

// activeRange returns the first and last date (YYYYMMDD) on which any service
// used by a trip actually runs, taking calendar_dates exceptions into account.
// Services without trips or without any active day are ignored. Both results
// are empty if no trip has an active day.
func activeRange(feed *gtfsparser.Feed) (start, end string) {
	seen := make(map[*gtfs.Service]bool)
	var first, last time.Time

	for _, trip := range feed.Trips {
		svc := trip.Service
		if svc == nil || seen[svc] {
			continue
		}
		seen[svc] = true

		if d := svc.GetFirstActiveDate(); !d.IsEmpty() {
			if t := d.GetTime(); first.IsZero() || t.Before(first) {
				first = t
			}
		}
		if d := svc.GetLastActiveDate(); !d.IsEmpty() {
			if t := d.GetTime(); last.IsZero() || t.After(last) {
				last = t
			}
		}
	}

	if first.IsZero() {
		return "", ""
	}
	return first.Format("20060102"), last.Format("20060102")
}

// routeTypeCounts returns "type=count" pairs for the log, ordered by type.
func routeTypeCounts(feed *gtfsparser.Feed) []string {
	counts := make(map[int16]int)
	for _, r := range feed.Routes {
		counts[r.Type]++
	}
	types := make([]int16, 0, len(counts))
	for t := range counts {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })

	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, strconv.Itoa(int(t))+"="+strconv.Itoa(counts[t]))
	}
	return out
}
