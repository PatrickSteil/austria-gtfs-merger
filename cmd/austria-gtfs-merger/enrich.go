package main

import (
	"fmt"
	"log"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/enrich"
	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/patrickbr/gtfsparser"
)

// runWheelchair runs the wheelchair step if OSM features are available. A
// missing or unreadable features file never aborts the merge: the step is
// reported as skipped with the reason and a warning is logged.
func runWheelchair(feed *gtfsparser.Feed, featuresPath string) enrich.Report {
	skip := func(reason string) enrich.Report {
		log.Printf("warn    wheelchair step skipped: %s", reason)
		return enrich.Report{Step: "wheelchair", Skipped: reason}
	}
	if featuresPath == "" {
		return skip("no --osm-features file given")
	}
	feats, err := osmfeat.Read(featuresPath)
	if err != nil {
		return skip(fmt.Sprintf("cannot read %s: %v", featuresPath, err))
	}
	if len(feats) == 0 {
		return skip(fmt.Sprintf("%s contains no features", featuresPath))
	}
	return enrich.Wheelchair(feed, feats, enrich.DefaultWheelchairOptions())
}
