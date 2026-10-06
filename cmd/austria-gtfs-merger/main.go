package main

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/download"
	"github.com/joho/godotenv"
	"github.com/patrickbr/gtfsparser"
	gtfs "github.com/patrickbr/gtfsparser/gtfs"
	"github.com/patrickbr/gtfswriter"
	flag "github.com/spf13/pflag"
)

var version = "dev"

func main() {
	godotenv.Load()

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "merger (C) Patrick Steil <patrick@steil.dev>\nVersion %s\nUsage:\n", version)
		flag.PrintDefaults()
	}

	downloadFlag := flag.BoolP("download", "d", false, "Download GTFS datasets")
	outputFlag := flag.StringP("output", "o", "merged.zip", "Output path (directory or .zip) for merged GTFS")
	dirFlag := flag.StringP("dir", "", "gtfs_feeds", "GTFS directory")
	verboseFlag := flag.BoolP("verbose", "v", false, "Verbose output")
	warningFlag := flag.BoolP("warning", "w", false, "Show all warnings while reading GTFS")
	threadsFlag := flag.IntP("threads", "t", 0, "Number of parallel downloads (0 = MAX_WORKERS or number of CPUs)")
	dropShapesFlag := flag.BoolP("drop-shapes", "s", false, "Drop shapes.txt data")
	dropErrFlag := flag.BoolP("drop-erroneous", "e", false, "Drop erroneous GTFS entities")
	manifestFlag := flag.StringP("manifest", "m", "versions.json", "Path to version manifest JSON")
	forceFlag := flag.BoolP("force", "f", false, "Force re-merge even if no new data was downloaded")
	statsFlag := flag.String("stats", "merge-stats.json", "File holding the previous run's feed statistics (empty disables the comparison)")
	skipChecksFlag := flag.Bool("skip-checks", false, "Write the output even if the sanity checks fail")
	var lim Limits
	flag.IntVar(&lim.MinAgencies, "min-agencies", 50, "Sanity check: minimum number of agencies (0 disables)")
	flag.IntVar(&lim.MinTrips, "min-trips", 500000, "Sanity check: minimum number of trips (0 disables)")
	flag.IntVar(&lim.MinRailRoutes, "min-rail-routes", 200, "Sanity check: minimum number of rail routes (0 disables)")
	flag.IntVar(&lim.MaxDropPct, "max-drop", 20, "Sanity check: maximum decrease in percent vs. the previous run (0 disables)")
	allowParseErrFlag := flag.Bool("allow-parse-errors", false, "Continue merging if some feeds fail to parse (default: abort)")

	flag.Parse()

	log.SetFlags(0)

	username, password := credentials()

	maxWorkers := *threadsFlag
	if maxWorkers <= 0 {
		if val, err := strconv.Atoi(os.Getenv("MAX_WORKERS")); err == nil && val > 0 {
			maxWorkers = val
		} else {
			maxWorkers = runtime.NumCPU()
		}
	}

	if *verboseFlag {
		log.Printf("using   %d worker(s) for downloads", maxWorkers)
	}

	if err := os.MkdirAll(*dirFlag, os.ModePerm); err != nil {
		log.Fatalf("error   creating directory: %v", err)
	}

	changed := *forceFlag

	if *downloadFlag && (username == "" || password == "") {
		log.Fatalf("error   DBP_USERNAME and DBP_PASSWORD must be set (env or .env) to download")
	}

	// feedFiles, when non-nil, is the exact set of zips to merge. Set in
	// download mode so superseded versions left in the directory are ignored.
	var feedFiles []string

	if *downloadFlag {
		manifest, err := download.LoadManifest(*manifestFlag)
		if err != nil {
			log.Printf("warn    could not load manifest (%v); treating all datasets as new", err)
		}

		result, err := download.DownloadAllDatasets(
			username,
			password,
			*dirFlag,
			maxWorkers,
			*verboseFlag,
			*forceFlag,
			manifest,
		)
		if err != nil {
			log.Fatalf("error   downloading datasets: %v", err)
		}

		if len(result.NewFiles) > 0 {
			log.Printf("new     %d file(s): %s", len(result.NewFiles), strings.Join(result.NewFiles, ", "))
		}

		// Persist progress even if some downloads failed: the manifest only
		// lists versions we actually have, so failed ones are retried next run.
		if result.Changed {
			changed = true
			if err := download.SaveManifest(*manifestFlag, result.UpdatedManifest); err != nil {
				log.Printf("warn    could not save manifest: %v", err)
			} else {
				log.Printf("saved   manifest to %s", *manifestFlag)
			}
		} else if !*forceFlag {
			log.Printf("info    no new GTFS versions found")
		}

		if len(result.Failed) > 0 {
			log.Fatalf("error   %d download(s) failed, not merging an incomplete feed: %s",
				len(result.Failed), strings.Join(result.Failed, ", "))
		}

		for _, e := range result.UpdatedManifest {
			feedFiles = append(feedFiles, filepath.Join(*dirFlag, e.OriginalName))
		}
	}

	if !changed {
		log.Printf("done    nothing to do (use --force to re-merge anyway)")
		os.Exit(0)
	}

	// ---- Merge ----

	files := feedFiles
	if files == nil {
		var err error
		files, err = filepath.Glob(filepath.Join(*dirFlag, "*.zip"))
		if err != nil {
			log.Fatalf("error   listing zip files: %v", err)
		}
	}
	if len(files) == 0 {
		log.Fatalf("error   no .zip files found in %s", *dirFlag)
	}

	sort.Strings(files)

	// Some datasets (e.g. Styria) are archives of GTFS zips rather than GTFS
	// themselves; unpack those so each inner feed gets parsed.
	tmpDir, err := os.MkdirTemp("", "gtfs-nested-")
	if err != nil {
		log.Fatalf("error   creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	files, err = expandNestedZips(files, tmpDir)
	if err != nil {
		log.Fatalf("error   unpacking nested zips: %v", err)
	}

	log.Printf("found   %d zip file(s), parsing sequentially...", len(files))

	opts := gtfsparser.ParseOptions{
		DropShapes:    *dropShapesFlag,
		DropErroneous: *dropErrFlag,
		ShowWarnings:  *warningFlag,
	}

	feed := gtfsparser.NewFeed()
	feed.SetParseOpts(opts)

	parseErrors := 0
	var problems []string
	for i, file := range files {
		if *verboseFlag {
			log.Printf("parse   (%d/%d) %s", i+1, len(files), filepath.Base(file))
		}
		before := len(feed.Trips)
		if err := feed.Parse(file); err != nil {
			log.Printf("error   parsing %s: %v", file, err)
			parseErrors++
			continue
		}
		if added := len(feed.Trips) - before; added == 0 {
			problems = append(problems, fmt.Sprintf("%s contributed no trips", filepath.Base(file)))
		} else if *verboseFlag {
			log.Printf("parse   %s added %d trip(s)", filepath.Base(file), added)
		}
	}

	if parseErrors > 0 {
		if !*allowParseErrFlag {
			log.Fatalf("error   %d file(s) failed to parse; refusing to write a partial feed (see --allow-parse-errors)", parseErrors)
		}
		log.Printf("warn    %d file(s) failed to parse and were skipped", parseErrors)
	}

	// ---- Enforce parent stations ----
	//
	// Austrian GTFS stop IDs are hierarchical: a platform/stop looks like
	// "at:42:2121:0:6" and its parent station would be "at:42:2121". Any
	// stop that is not already a station (location_type != 1) and has no
	// parent_station assigned gets a synthetic parent created from the first
	// three colon-delimited segments of its ID.
	syntheticCount := enforceParentStations(feed)
	if syntheticCount > 0 {
		log.Printf("info    created %d synthetic parent station(s)", syntheticCount)
	}

	cleaned := cleanHeadsigns(feed)
	if cleaned > 0 {
		log.Printf("info    cleaned %d garbage stop_headsign value(s)", cleaned)
	}

	log.Printf("done    parsing complete")
	log.Printf(
		"merged feed: agencies=%d stops=%d routes=%d trips=%d fare_attributes=%d",
		len(feed.Agencies),
		len(feed.Stops),
		len(feed.Routes),
		len(feed.Trips),
		len(feed.FareAttributes),
	)

	// ---- Sanity checks ----
	//
	// Refuse to write (and thus publish) a feed that looks incomplete.
	cur := computeStats(feed)
	var prev *Stats
	if *statsFlag != "" {
		var err error
		if prev, err = loadStats(*statsFlag); err != nil {
			log.Printf("warn    could not load previous stats: %v", err)
		}
	}
	problems = append(problems, checkStats(cur, prev, lim)...)
	log.Printf("stats   agencies=%d stops=%d routes=%d (rail=%d) trips=%d",
		cur.Agencies, cur.Stops, cur.Routes, cur.RailRoutes, cur.Trips)
	if len(problems) > 0 {
		for _, p := range problems {
			log.Printf("check   FAILED %s", p)
		}
		if !*skipChecksFlag {
			log.Fatalf("error   %d sanity check(s) failed; not writing output (see --skip-checks)", len(problems))
		}
		log.Printf("warn    continuing despite failed checks (--skip-checks)")
	}

	if *outputFlag == "" {
		return
	}

	if *verboseFlag {
		log.Printf("write   GTFS to %s", *outputFlag)
	}

	isZip := strings.HasSuffix(strings.ToLower(*outputFlag), ".zip")
	if isZip {
		// gtfswriter stats the target path before writing, so it must exist.
		f, err := os.Create(*outputFlag)
		if err != nil {
			log.Fatalf("error   creating output file: %v", err)
		}
		f.Close()
	} else if err := os.MkdirAll(*outputFlag, os.ModePerm); err != nil {
		log.Fatalf("error   creating output directory: %v", err)
	}

	writer := gtfswriter.Writer{Sorted: true}
	if err := writer.Write(feed, *outputFlag); err != nil {
		log.Fatalf("error   writing GTFS: %v", err)
	}

	log.Printf("done    GTFS written to %s", *outputFlag)

	if err := injectFeedInfo(*outputFlag, isZip); err != nil {
		log.Printf("warn    could not inject feed_info.txt: %v", err)
	} else {
		log.Printf("done    injected feed_info.txt into %s", *outputFlag)
	}

	// Only a feed that passed the checks becomes the new baseline.
	if *statsFlag != "" && len(problems) == 0 {
		if err := saveStats(*statsFlag, cur); err != nil {
			log.Printf("warn    could not save stats: %v", err)
		}
	}
}

// expandNestedZips replaces every zip that has no top-level agency.txt but
// contains .zip entries with those inner zips, extracted into tmpDir. All
// other files are passed through unchanged. Unreadable archives are passed
// through too, so the parse step reports them.
func expandNestedZips(files []string, tmpDir string) ([]string, error) {
	var out []string
	for i, file := range files {
		inner, err := extractNested(file, filepath.Join(tmpDir, strconv.Itoa(i)))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		if inner == nil {
			out = append(out, file)
			continue
		}
		log.Printf("nested  %s contains %d feed(s)", filepath.Base(file), len(inner))
		out = append(out, inner...)
	}
	return out, nil
}

// extractNested returns the extracted inner zips of file, or nil if file is a
// regular GTFS archive (or cannot be opened).
func extractNested(file, dest string) ([]string, error) {
	r, err := zip.OpenReader(file)
	if err != nil {
		return nil, nil
	}
	defer r.Close()

	var inner []*zip.File
	for _, f := range r.File {
		if f.Name == "agency.txt" {
			return nil, nil
		}
		if !f.FileInfo().IsDir() && strings.HasSuffix(strings.ToLower(f.Name), ".zip") {
			inner = append(inner, f)
		}
	}
	if len(inner) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}

	var paths []string
	for _, f := range inner {
		// Flatten to the base name: never trust paths inside the archive.
		path := filepath.Join(dest, filepath.Base(f.Name))
		if err := extractFile(f, path); err != nil {
			return nil, fmt.Errorf("extract %s: %w", f.Name, err)
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func extractFile(f *zip.File, path string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// credentials reads the DBP login from DBP_USERNAME/DBP_PASSWORD. The bare
// USERNAME/PASSWORD names are still accepted as a fallback, but USERNAME is
// commonly set by the OS to something unrelated, so they are discouraged.
func credentials() (string, string) {
	u, p := os.Getenv("DBP_USERNAME"), os.Getenv("DBP_PASSWORD")
	if u == "" && p == "" {
		u, p = os.Getenv("USERNAME"), os.Getenv("PASSWORD")
		if u != "" || p != "" {
			log.Printf("warn    USERNAME/PASSWORD are deprecated; rename them to DBP_USERNAME/DBP_PASSWORD")
		}
	}
	return u, p
}

// cleanHeadsigns removes stop_headsign values from stop times (and trip
// headsigns from trips) that are clearly garbage: blank/whitespace-only
// strings, pure integer strings ("0", "1", …), or strings composed entirely
// of punctuation/symbols. Clearing them lets consumers fall back to the
// trip-level headsign or their own destination logic rather than displaying
// meaningless values.
//
// Returns the total number of headsign fields that were cleared.
func cleanHeadsigns(feed *gtfsparser.Feed) int {
	cleaned := 0
	for _, trip := range feed.Trips {
		// Clean the trip-level headsign (*string). Use a pointer to "" rather
		// than nil — gtfswriter dereferences this unconditionally.
		if trip.Headsign != nil && *trip.Headsign != "" && isGarbageHeadsign(*trip.Headsign) {
			empty := ""
			trip.Headsign = &empty
			cleaned++
		}
		// Clean per-stop-time headsigns. SetHeadsign(nil) causes a nil-pointer
		// dereference in gtfswriter; use a pointer to "" instead — GTFS treats
		// an empty stop_headsign as "no headsign".
		for i := range trip.StopTimes {
			if h := trip.StopTimes[i].Headsign(); h != nil && *h != "" && isGarbageHeadsign(*h) {
				empty := ""
				trip.StopTimes[i].SetHeadsign(&empty)
				cleaned++
			}
		}
	}
	return cleaned
}

// isGarbageHeadsign returns true for headsign strings that carry no useful
// destination information:
//   - empty or whitespace-only
//   - a bare non-negative integer ("0", "1", "42", …)
//   - composed entirely of non-letter, non-digit characters (pure punctuation)
func isGarbageHeadsign(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	// Pure integer check.
	if _, err := strconv.Atoi(s); err == nil {
		return true
	}
	// All non-alphanumeric: symbols, punctuation, etc.
	allSymbols := true
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			allSymbols = false
			break
		}
	}
	return allSymbols
}

// enforceParentStations gives every stop that
//   - is neither a station (Location_type 1) nor a boarding area (4), whose
//     parent must be a platform rather than a station, and
//   - has no Parent_station pointer set,
//
// a parent station whose ID is derived by parentStationID (e.g.
// "at:42:2121:0:6" -> "Pat:42:2121"). An existing station with that ID is
// reused; if the ID is taken by a non-station stop, the child is left alone.
// Otherwise a station is synthesised: it copies the first child's (in ID
// order) descriptive attributes and is placed at the centroid of all its
// children. Stops are processed in sorted order so the output is
// deterministic.
//
// Returns the number of newly created synthetic stations.
func enforceParentStations(feed *gtfsparser.Feed) int {
	stopIDs := make([]string, 0, len(feed.Stops))
	for id := range feed.Stops {
		stopIDs = append(stopIDs, id)
	}
	sort.Strings(stopIDs)

	// Group the orphans by derived parent ID, preserving sorted order.
	var parentOrder []string
	orphans := make(map[string][]*gtfs.Stop)
	for _, id := range stopIDs {
		stop := feed.Stops[id]
		if stop.Location_type == 1 || stop.Location_type == 4 || stop.Parent_station != nil {
			continue
		}
		parentID := parentStationID(id)
		if parentID == "" || parentID == id {
			continue
		}
		if _, ok := orphans[parentID]; !ok {
			parentOrder = append(parentOrder, parentID)
		}
		orphans[parentID] = append(orphans[parentID], stop)
	}

	created := 0
	for _, parentID := range parentOrder {
		children := orphans[parentID]

		parent, exists := feed.Stops[parentID]
		if exists && parent.Location_type != 1 {
			continue // ID clash with a non-station stop; don't misuse it
		}
		if !exists {
			first := children[0]
			var lat, lon float64
			n := 0
			for _, c := range children {
				if c.HasLatLon() {
					lat += float64(c.Lat)
					lon += float64(c.Lon)
					n++
				}
			}
			parent = &gtfs.Stop{
				Id:                  parentID,
				Name:                first.Name,
				Desc:                first.Desc,
				Lat:                 first.Lat,
				Lon:                 first.Lon,
				Zone_id:             first.Zone_id,
				Url:                 first.Url,
				Location_type:       1,
				Timezone:            first.Timezone,
				Wheelchair_boarding: first.Wheelchair_boarding,
			}
			if n > 0 {
				parent.Lat = float32(lat / float64(n))
				parent.Lon = float32(lon / float64(n))
			}
			feed.Stops[parentID] = parent
			created++
		}
		for _, c := range children {
			c.Parent_station = parent
		}
	}

	return created
}

// parentStationID derives the parent station ID from a stop ID.
// Stop IDs follow the Austrian GTFS convention "at:42:2121:0:6"; the
// corresponding parent station ID uses the same numeric segments but
// replaces the leading "at" with "Pat":
//
//	"at:42:2121:0:6"  ->  "Pat:42:2121"
//	"at:42:2121"      ->  "Pat:42:2121"
//	"simple"          ->  ""            (not enough segments)
func parentStationID(stopID string) string {
	parts := strings.SplitN(stopID, ":", 4)
	if len(parts) < 3 {
		return ""
	}
	// Replace the leading country/namespace segment with "Pat".
	return "Pat:" + parts[1] + ":" + parts[2]
}

// injectFeedInfo adds (or replaces) feed_info.txt in the merged output with a
// feed_version set to the current UTC timestamp (YYYYMMDD_HHMMSS). The start
// and end dates are left blank: the real validity window is defined by the
// calendars, not by the build time. For a directory output the file is written
// directly; for a zip, the archive is rewritten.
func injectFeedInfo(out string, isZip bool) error {
	feedVersion := time.Now().UTC().Format("20060102_150405")

	content := "feed_publisher_name,feed_publisher_url,feed_lang,feed_version,feed_start_date,feed_end_date\n"
	content += fmt.Sprintf("austria-gtfs-merger,https://github.com/PatrickSteil/austria-gtfs-merger,de,%s,,\n", feedVersion)

	if !isZip {
		return os.WriteFile(filepath.Join(out, "feed_info.txt"), []byte(content), 0o644)
	}

	r, err := zip.OpenReader(out)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	tmp := out + ".tmp"
	outFile, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			outFile.Close()
			os.Remove(tmp)
		}
	}()

	w := zip.NewWriter(outFile)
	for _, f := range r.File {
		if f.Name == "feed_info.txt" {
			continue
		}
		if err := w.Copy(f); err != nil { // raw copy, no recompression
			return fmt.Errorf("copy entry %s: %w", f.Name, err)
		}
	}

	fw, err := w.Create("feed_info.txt")
	if err != nil {
		return err
	}
	if _, err := io.WriteString(fw, content); err != nil {
		return fmt.Errorf("write feed_info.txt: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("finalise zip: %w", err)
	}
	if err := outFile.Close(); err != nil {
		return fmt.Errorf("close tmp file: %w", err)
	}
	ok = true
	return os.Rename(tmp, out)
}
