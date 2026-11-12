// Package enrich adds derived data (platform names, route metadata,
// wheelchair accessibility) to a merged GTFS feed. Every step only fills
// empty fields or fixes clearly recognised patterns and reports what it did.
package enrich

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	maxExamples  = 20
	maxConflicts = 50
)

// Report describes what one enrichment step did.
type Report struct {
	Step      string         `json:"step"`
	Changed   int            `json:"changed"`
	Examples  []string       `json:"examples,omitempty"`
	Conflicts []string       `json:"conflicts,omitempty"`
	Counts    map[string]int `json:"counts,omitempty"`
	// Skipped is non-empty when the step did not run; it holds the reason.
	Skipped string `json:"skipped,omitempty"`
}

// Example records an illustrative change (at most 20 are kept).
func (r *Report) Example(format string, args ...any) {
	if len(r.Examples) < maxExamples {
		r.Examples = append(r.Examples, fmt.Sprintf(format, args...))
	}
}

// Conflict records a case where source data and derived data disagree and the
// source value was kept (at most 50 are kept).
func (r *Report) Conflict(format string, args ...any) {
	if len(r.Conflicts) < maxConflicts {
		r.Conflicts = append(r.Conflicts, fmt.Sprintf(format, args...))
	}
}

// Count adds n to the named counter.
func (r *Report) Count(name string, n int) {
	if r.Counts == nil {
		r.Counts = make(map[string]int)
	}
	r.Counts[name] += n
}

// WriteReports writes the reports as indented JSON. An empty path is a no-op.
func WriteReports(path string, reports []Report) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Steps selects which enrichment steps run.
type Steps struct {
	Platforms, Routes, Wheelchair bool
}

// ParseSteps parses "all" or a comma separated list of steig, routes and
// wheelchair. The empty string selects nothing.
func ParseSteps(spec string) (Steps, error) {
	var s Steps
	for _, part := range strings.Split(spec, ",") {
		switch strings.TrimSpace(part) {
		case "":
		case "all":
			s = Steps{true, true, true}
		case "steig":
			s.Platforms = true
		case "routes":
			s.Routes = true
		case "wheelchair":
			s.Wheelchair = true
		default:
			return Steps{}, fmt.Errorf("unknown enrichment step %q (want steig, routes, wheelchair or all)", part)
		}
	}
	return s, nil
}
