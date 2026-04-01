package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/auth"
	"github.com/PatrickSteil/austria-gtfs-merger/internal/download"
	"github.com/joho/godotenv"
	"github.com/patrickbr/gtfsparser"
	"github.com/patrickbr/gtfswriter"
	flag "github.com/spf13/pflag"
)

var version = "dev"

type Job struct {
	id   string
	year string
	name string
}

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
	warningFlag := flag.BoolP("warning", "w", false, "Show all warning while reading GTFS")
	threadsFlag := flag.IntP("threads", "t", 0, "Number of worker threads (download only - 0 means all threads)")
	dropShapesFlag := flag.BoolP("drop-shapes", "s", false, "Drop shapes.txt data")
	dropErrFlag := flag.BoolP("drop-erroneous", "e", false, "Drop erroneous GTFS entities")

	flag.Parse()

	log.SetFlags(0)

	username := os.Getenv("USERNAME")
	password := os.Getenv("PASSWORD")

	maxWorkers := *threadsFlag
	if maxWorkers <= 0 {
		maxWorkersEnv := os.Getenv("MAX_WORKERS")
		if val, err := strconv.Atoi(maxWorkersEnv); err == nil && val > 0 {
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

	if *downloadFlag {
		aut := auth.NewAuth(username, password)

		log.Printf("fetch   datasets from DBP...")
		datasets, err := auth.GetDatasets(aut)
		if err != nil {
			log.Fatalf("error   fetching datasets: %v", err)
		}

		var jobs []Job
		for _, ds := range datasets {
			if !auth.IsGTFS(ds) || len(ds.ActiveVersions) == 0 {
				continue
			}
			v := ds.ActiveVersions[0]
			jobs = append(jobs, Job{
				id:   ds.ID,
				year: v.Year,
				name: v.DataSetVersion.File.OriginalName,
			})
		}

		log.Printf("found   %d GTFS dataset(s), downloading...", len(jobs))

		var wg sync.WaitGroup
		jobChan := make(chan Job)

		for i := 0; i < maxWorkers; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				for job := range jobChan {
					if *verboseFlag {
						log.Printf("worker  %d downloading %s", workerID, job.name)
					}
					download.DownloadDataset(aut, job.id, job.year, job.name, *dirFlag)
				}
			}(i)
		}

		for _, j := range jobs {
			jobChan <- j
		}
		close(jobChan)
		wg.Wait()

		log.Printf("done    downloads complete")
	}

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
	if *outputFlag != "" {
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

		writer := gtfswriter.Writer{
			Sorted: true,
		}

		err := writer.Write(feed, *outputFlag)
		if err != nil {
			log.Fatalf("error   writing GTFS: %v", err)
		}

		log.Printf("done    GTFS written to %s", *outputFlag)
	}
}
