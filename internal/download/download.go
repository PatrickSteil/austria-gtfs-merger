package download

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/auth"
	"github.com/PatrickSteil/austria-gtfs-merger/internal/models"
)

type Job struct {
	id   string
	year string
	name string
}

// LoadManifest reads the persisted version manifest from disk.
// Returns an empty manifest (never nil) if the file does not exist.
func LoadManifest(path string) (models.Manifest, error) {
	m := make(models.Manifest)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("manifest parse error: %w", err)
	}
	return m, nil
}

// SaveManifest writes the version manifest to disk atomically.
func SaveManifest(path string, m models.Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DownloadDataset fetches a single GTFS zip if it is not already present on
// disk. Returns true if the file was freshly downloaded, false if it was
// already present (skipped).
func DownloadDataset(a *auth.DBPAuth, datasetID string, year string, filename string, outDir string) bool {
	os.MkdirAll(outDir, os.ModePerm)

	path := outDir + "/" + filename
	if _, err := os.Stat(path); err == nil {
		log.Printf("skip    %s (already exists)", path)
		return false
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
			return false
		}
		defer file.Close()

		io.Copy(file, resp.Body)
		resp.Body.Close()

		log.Printf("saved   %s", path)
		return true
	}

	log.Printf("failed  %s: all 3 attempts exhausted", filename)
	return false
}

// DownloadResult is returned by DownloadAllDatasets.
type DownloadResult struct {
	// UpdatedManifest is the new manifest reflecting all current versions.
	UpdatedManifest models.Manifest
	// Changed is true if at least one dataset was newly downloaded.
	Changed bool
	// NewFiles lists the filenames that were freshly downloaded.
	NewFiles []string
}

// DownloadAllDatasets fetches all GTFS datasets, skipping any whose version
// matches the provided manifest. It returns a DownloadResult that callers
// use to decide whether a re-merge is necessary and to persist the updated
// manifest.
func DownloadAllDatasets(username, password string, outDir string, maxWorkers int, verbose bool, manifest models.Manifest) DownloadResult {
	aut := auth.NewAuth(username, password)

	log.Printf("fetch   datasets from DBP...")
	datasets, err := auth.GetDatasets(aut)
	if err != nil {
		log.Fatalf("error   fetching datasets: %v", err)
	}

	type jobWithMeta struct {
		Job
		isNew bool // version differs from manifest
	}

	newManifest := make(models.Manifest)
	var jobs []jobWithMeta

	for _, ds := range datasets {
		if !auth.IsGTFS(ds) || len(ds.ActiveVersions) == 0 {
			continue
		}
		v := ds.ActiveVersions[0]
		entry := models.ManifestEntry{
			Year:         v.Year,
			OriginalName: v.DataSetVersion.File.OriginalName,
		}
		newManifest[ds.ID] = entry

		prev, seen := manifest[ds.ID]
		isNew := !seen || prev.Year != entry.Year || prev.OriginalName != entry.OriginalName

		jobs = append(jobs, jobWithMeta{
			Job: Job{
				id:   ds.ID,
				year: entry.Year,
				name: entry.OriginalName,
			},
			isNew: isNew,
		})
	}

	log.Printf("found   %d GTFS dataset(s), downloading...", len(jobs))

	var (
		wg          sync.WaitGroup
		jobChan     = make(chan jobWithMeta)
		downloadedN int64 // atomic counter
		mu          sync.Mutex
		newFiles    []string
	)

	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for job := range jobChan {
				if !job.isNew {
					if verbose {
						log.Printf("worker  %d: %s unchanged, skipping download", workerID, job.name)
					} else {
						log.Printf("skip    %s (version unchanged)", job.name)
					}
					continue
				}
				if verbose {
					log.Printf("worker  %d downloading %s (new version)", workerID, job.name)
				}
				downloaded := DownloadDataset(aut, job.id, job.year, job.name, outDir)
				if downloaded {
					atomic.AddInt64(&downloadedN, 1)
					mu.Lock()
					newFiles = append(newFiles, job.name)
					mu.Unlock()
				}
			}
		}(i)
	}

	for _, j := range jobs {
		jobChan <- j
	}
	close(jobChan)
	wg.Wait()

	log.Printf("done    downloads complete (%d new)", atomic.LoadInt64(&downloadedN))

	return DownloadResult{
		UpdatedManifest: newManifest,
		Changed:         atomic.LoadInt64(&downloadedN) > 0,
		NewFiles:        newFiles,
	}
}
