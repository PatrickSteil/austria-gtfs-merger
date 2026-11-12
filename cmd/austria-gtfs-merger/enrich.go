package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/enrich"
	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/patrickbr/gtfsparser"
	"github.com/patrickbr/gtfswriter"
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

// validateEnrichFlags keeps OSM-derived data out of the main feed: the
// wheelchair step writes a separate output (licensed under the ODbL), and that
// output only makes sense together with the step.
func validateEnrichFlags(steps enrich.Steps, wheelchairOut string) error {
	if steps.Wheelchair && wheelchairOut == "" {
		return errors.New("the wheelchair step adds OpenStreetMap data and needs --wheelchair-output, so the main feed stays free of it")
	}
	if wheelchairOut != "" && !steps.Wheelchair {
		return errors.New("--wheelchair-output needs the wheelchair step (--enrich wheelchair or all)")
	}
	return nil
}

// writeFeed writes the feed as a zip (or a directory) and injects feed_info.txt;
// with osmUsed it also adds the OpenStreetMap attribution.
func writeFeed(feed *gtfsparser.Feed, out string, osmUsed bool) error {
	isZip := strings.HasSuffix(strings.ToLower(out), ".zip")
	if isZip {
		// gtfswriter stats the target path before writing, so it must exist.
		f, err := os.Create(out)
		if err != nil {
			return fmt.Errorf("creating output file: %w", err)
		}
		f.Close()
	} else if err := os.MkdirAll(out, os.ModePerm); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	writer := gtfswriter.Writer{Sorted: true}
	if err := writer.Write(feed, out); err != nil {
		return fmt.Errorf("writing GTFS: %w", err)
	}
	log.Printf("done    GTFS written to %s", out)

	startDate, endDate := activeRange(feed)
	log.Printf("info    service dates %s - %s", startDate, endDate)
	if err := injectFeedInfo(out, isZip, startDate, endDate, osmUsed); err != nil {
		log.Printf("warn    could not inject feed_info.txt: %v", err)
	} else {
		log.Printf("done    injected feed_info.txt into %s", out)
	}
	return nil
}
