package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/enrich"
	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
	"github.com/patrickbr/gtfsparser"
	"github.com/patrickbr/gtfswriter"
)

// osmInputs are the OSM files and the output directory of the OSM steps.
type osmInputs struct {
	FeaturesPath string // osm-features.json.gz from osm-extract
	RoutesPath   string // osm-routes.json.gz from osm-extract (for the osmroutes step)
	SidecarDir   string // receives osm-stop-attributes.csv.gz and osm-qa-report.json
}

// runOSM runs the selected OSM steps on the feed. The stops are matched to OSM
// once and the result is shared by all steps. A missing, unreadable or
// outdated features file never aborts the merge: nothing is run, a warning is
// logged and used is false. A missing routes file skips only the osmroutes step.
func runOSM(feed *gtfsparser.Feed, steps enrich.Steps, in osmInputs, palette map[string]bool) (reports []enrich.Report, used bool) {
	skip := func(step, reason string) enrich.Report {
		log.Printf("warn    %s step skipped: %s", step, reason)
		return enrich.Report{Step: step, Skipped: reason}
	}
	if in.FeaturesPath == "" {
		return []enrich.Report{skip("osm", "no --osm-features file given")}, false
	}
	feats, err := osmfeat.Read(in.FeaturesPath)
	if err != nil {
		return []enrich.Report{skip("osm", fmt.Sprintf("cannot read %s: %v", in.FeaturesPath, err))}, false
	}
	if len(feats) == 0 {
		return []enrich.Report{skip("osm", fmt.Sprintf("%s contains no features", in.FeaturesPath))}, false
	}

	matches := enrich.Match(feed, feats, enrich.DefaultWheelchairOptions())
	if steps.Wheelchair {
		reports = append(reports, enrich.ApplyWheelchair(feed, matches))
	}
	if steps.PlatformCodes {
		reports = append(reports, enrich.PlatformCodes(matches))
	}

	var rels []osmfeat.RouteRel
	var rm enrich.RouteMatchResult
	if steps.OSMRoutes {
		switch {
		case in.RoutesPath == "":
			reports = append(reports, skip("osmroutes", "no --osm-routes file given"))
		default:
			if rels, err = osmfeat.ReadRoutes(in.RoutesPath); err != nil {
				rels = nil
				reports = append(reports, skip("osmroutes", fmt.Sprintf("cannot read %s: %v", in.RoutesPath, err)))
			} else {
				var rep enrich.Report
				rep, rm = enrich.OSMRoutes(feed, rels, palette)
				reports = append(reports, rep)
			}
		}
	}
	if steps.Entrances {
		reports = append(reports, enrich.Entrances(feed, feats))
	}

	if in.SidecarDir != "" {
		if err := os.MkdirAll(in.SidecarDir, 0o755); err != nil {
			log.Printf("warn    cannot create %s: %v", in.SidecarDir, err)
		} else {
			side := filepath.Join(in.SidecarDir, "osm-stop-attributes.csv.gz")
			if err := enrich.WriteSidecar(side, enrich.SidecarRows(matches, feats)); err != nil {
				log.Printf("warn    could not write %s: %v", side, err)
			}
			qa := filepath.Join(in.SidecarDir, "osm-qa-report.json")
			if err := enrich.WriteQA(qa, enrich.QA(feed, feats, matches, rels, rm)); err != nil {
				log.Printf("warn    could not write %s: %v", qa, err)
			}
		}
	}
	return reports, true
}

// validateEnrichFlags keeps OSM-derived data out of the main feed: every OSM
// step writes a separate output (licensed under the ODbL), and that output only
// makes sense together with such a step.
func validateEnrichFlags(steps enrich.Steps, osmOutput string) error {
	if steps.NeedsOSM() && osmOutput == "" {
		return errors.New("the OpenStreetMap steps (wheelchair, platformcodes, osmroutes, entrances) need --osm-output, so the main feed stays free of OSM data")
	}
	if osmOutput != "" && !steps.NeedsOSM() {
		return errors.New("--osm-output needs at least one OpenStreetMap step (wheelchair, platformcodes, osmroutes, entrances or all)")
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
