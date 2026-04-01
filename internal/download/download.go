package download

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/auth"
)

type Job struct {
	id   string
	year string
	name string
}

func DownloadDataset(a *auth.DBPAuth, datasetID string, year string, filename string, outDir string) {
	os.MkdirAll(outDir, os.ModePerm)

	path := outDir + "/" + filename
	if _, err := os.Stat(path); err == nil {
		log.Printf("skip    %s (already exists)", path)
		return
	}

	url := fmt.Sprintf("%s/api/public/v1/data-sets/%s/%s/file", auth.DBP_BASE, datasetID, year)

	for attempt := 1; attempt <= 3; attempt++ {
		headers, _ := a.Header()

		req, _ := http.NewRequest("GET", url, nil)
		req.Header = headers

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("retry   %s (attempt %d/3): %v", filename, attempt, err)
			time.Sleep(2 * time.Second)
			continue
		}

		if resp.StatusCode == 401 {
			log.Printf("retry   %s (attempt %d/3): 401 unauthorized, refreshing token", filename, attempt)
			resp.Body.Close()
			continue
		}

		if resp.StatusCode != 200 {
			log.Printf("retry   %s (attempt %d/3): unexpected status %s", filename, attempt, resp.Status)
			resp.Body.Close()
			continue
		}

		file, err := os.Create(path)
		if err != nil {
			log.Printf("failed  %s: %v", filename, err)
			resp.Body.Close()
			return
		}
		defer file.Close()

		io.Copy(file, resp.Body)
		resp.Body.Close()

		log.Printf("saved   %s", path)
		return
	}

	log.Printf("failed  %s: all 3 attempts exhausted", filename)
}

func DownloadAllDatasets(username, password string, outDir string, maxWorkers int, verbose bool) {
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
				if verbose {
					log.Printf("worker  %d downloading %s", workerID, job.name)
				}
				DownloadDataset(aut, job.id, job.year, job.name, outDir)
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
