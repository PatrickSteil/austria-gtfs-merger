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

	"github.com/PatrickSteil/austria-gtfs-merger/internal/download"
	"github.com/joho/godotenv"
	"github.com/patrickbr/gtfsparser"
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

	downloadFlag    := flag.BoolP("download", "d", false, "Download GTFS datasets")
	outputFlag      := flag.StringP("output", "o", "merged.zip", "Output path (directory or .zip) for merged GTFS")
	dirFlag         := flag.StringP("dir", "", "gtfs_feeds", "GTFS directory")
	verboseFlag     := flag.BoolP("verbose", "v", false, "Verbose output")
	warningFlag     := flag.BoolP("warning", "w", false, "Show all warnings while reading GTFS")
	threadsFlag     := flag.IntP("threads", "t", 0, "Number of worker threads (download only - 0 means all threads)")
	dropShapesFlag  := flag.BoolP("drop-shapes", "s", false, "Drop shapes.txt data")
	dropErrFlag     := flag.BoolP("drop-erroneous", "e", false, "Drop erroneous GTFS entities")
	manifestFlag    := flag.StringP("manifest", "m", "versions.json", "Path to version manifest JSON")
	forceFlag       := flag.BoolP("force", "f", false, "Force re-merge even if no new data was downloaded")

	flag.Parse()

	log.SetFlags(0)

	username := os.Getenv("USERNAME")
	password := os.Getenv("PASSWORD")

	maxWorkers := *threadsFlag
	if maxWorkers <= 0 {
		if val, err := strconv.Atoi(os.Getenv("MAX_WORKERS")); err == nil && val > 0 {
			maxWorkers = val
		} else {
			maxWorkers = runtime.NumCPU()
		}
	}
	if maxWorkers > runtime.NumCPU() {
		maxWorkers = runtime.NumCPU()
	}

	if *verboseFlag {
		log.Printf("using   %d worker(s) for downloads", maxWorkers)
	}

	if err := os.MkdirAll(*dirFlag, os.ModePerm); err != nil {
		log.Fatalf("error   creating directory: %v", err)
	}

	changed := *forceFlag // if --force, always re-merge

	if *downloadFlag {
		// Load the persisted manifest so we can skip unchanged datasets.
		manifest, err := download.LoadManifest(*manifestFlag)
		if err != nil {
			log.Printf("warn    could not load manifest (%v); treating all datasets as new", err)
		}

		result := download.DownloadAllDatasets(
			username,
			password,
			*dirFlag,
			maxWorkers,
			*verboseFlag,
			manifest,
		)

		if result.Changed {
			changed = true
			if len(result.NewFiles) > 0 {
				log.Printf("new     %d file(s): %s", len(result.NewFiles), strings.Join(result.NewFiles, ", "))
			}

			// Persist the updated manifest so the next run knows what we have.
			if err := download.SaveManifest(*manifestFlag, result.UpdatedManifest); err != nil {
				log.Printf("warn    could not save manifest: %v", err)
			} else {
				log.Printf("saved   manifest to %s", *manifestFlag)
			}
		} else {
			log.Printf("info    no new GTFS versions found")
		}
	}

	if !changed {
		log.Printf("done    nothing to do (use --force to re-merge anyway)")
		// Exit 0 — not an error, just no-op. The GH Actions workflow checks
		// the GTFS_CHANGED env var that the workflow sets from the manifest
		// diff, so this path is the "skip release" path.
		os.Exit(0)
	}

	// ---- Merge ----

	files, err := filepath.Glob(filepath.Join(*dirFlag, "*.zip"))
	if err != nil {
		log.Fatalf("error   listing zip files: %v", err)
	}
	if len(files) == 0 {
		log.Fatalf("error   no .zip files found in %s", *dirFlag)
	}

	sort.Strings(files)

	log.Printf("found   %d zip file(s), parsing sequentially...", len(files))

	opts := gtfsparser.ParseOptions{
		DropShapes:    *dropShapesFlag,
		DropErroneous: *dropErrFlag,
		ShowWarnings:  *warningFlag,
	}

	feed := gtfsparser.NewFeed()
	feed.SetParseOpts(opts)

	for i, file := range files {
		if *verboseFlag {
			log.Printf("parse   (%d/%d) %s", i+1, len(files), filepath.Base(file))
		}
		if err := feed.Parse(file); err != nil {
			log.Printf("error   parsing %s: %v", file, err)
			continue
		}
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

	if *outputFlag == "" {
		return
	}

	if *verboseFlag {
		log.Printf("write   GTFS to %s", *outputFlag)
	}

	if strings.HasSuffix(strings.ToLower(*outputFlag), ".zip") {
		f, err := os.Create(*outputFlag)
		if err != nil {
			log.Fatalf("error   creating output file: %v", err)
		}
		f.Close()
	}

	writer := gtfswriter.Writer{Sorted: true}
	if err := writer.Write(feed, *outputFlag); err != nil {
		log.Fatalf("error   writing GTFS: %v", err)
	}

	log.Printf("done    GTFS written to %s", *outputFlag)

	// Inject feed_info.txt with a feed_version timestamp so downloaders can
	// detect whether they already have the latest merged file.
	if strings.HasSuffix(strings.ToLower(*outputFlag), ".zip") {
		if err := injectFeedInfo(*outputFlag); err != nil {
			log.Printf("warn    could not inject feed_info.txt: %v", err)
		} else {
			log.Printf("done    injected feed_info.txt into %s", *outputFlag)
		}
	}
}

// injectFeedInfo adds (or replaces) a feed_info.txt entry inside the GTFS zip
// with a feed_version set to the current UTC timestamp (YYYYMMDD_HHMMSS).
// This gives any consumer a lightweight way to check "do I already have this
// version?" without downloading the whole file.
func injectFeedInfo(zipPath string) error {
	now := time.Now().UTC()
	feedVersion := now.Format("20060102_150405")
	feedDate    := now.Format("20060102")

	content := "feed_publisher_name,feed_publisher_url,feed_lang,feed_version,feed_start_date,feed_end_date\n"
	content += fmt.Sprintf("austria-gtfs-merger,https://github.com/PatrickSteil/austria-gtfs-merger,de,%s,%s,%s\n",
		feedVersion, feedDate, feedDate)

	// Read existing zip into memory.
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	tmp := zipPath + ".tmp"
	outFile, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}

	w := zip.NewWriter(outFile)

	// Copy all existing entries except any pre-existing feed_info.txt.
	for _, f := range r.File {
		if f.Name == "feed_info.txt" {
			continue
		}
		fw, err := w.CreateHeader(&f.FileHeader)
		if err != nil {
			outFile.Close()
			os.Remove(tmp)
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			os.Remove(tmp)
			return err
		}
		io.Copy(fw, rc)
		rc.Close()
	}

	// Write the new feed_info.txt.
	fw, err := w.Create("feed_info.txt")
	if err != nil {
		outFile.Close()
		os.Remove(tmp)
		return err
	}
	fmt.Fprint(fw, content)

	w.Close()
	outFile.Close()
	r.Close()

	return os.Rename(tmp, zipPath)
}
