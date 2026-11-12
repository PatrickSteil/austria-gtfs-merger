package main

import (
	"testing"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/enrich"
)

// The main feed must never contain OSM-derived data: the wheelchair step is
// only allowed together with a separate output file.
func TestValidateEnrichFlags(t *testing.T) {
	for name, c := range map[string]struct {
		steps   enrich.Steps
		out     string
		wantErr bool
	}{
		"no wheelchair":               {enrich.Steps{Platforms: true, Routes: true}, "", false},
		"wheelchair with output":      {enrich.Steps{Wheelchair: true}, "osm.zip", false},
		"wheelchair without output":   {enrich.Steps{Wheelchair: true}, "", true},
		"output without wheelchair":   {enrich.Steps{Routes: true}, "osm.zip", true},
		"nothing at all":              {enrich.Steps{}, "", false},
		"all steps with output":       {enrich.Steps{Platforms: true, Routes: true, Wheelchair: true}, "osm.zip", false},
		"all steps without an output": {enrich.Steps{Platforms: true, Routes: true, Wheelchair: true}, "", true},
	} {
		err := validateEnrichFlags(c.steps, c.out)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", name, err, c.wantErr)
		}
	}
}
