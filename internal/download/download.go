package download

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/auth"
	"github.com/PatrickSteil/austria-gtfs-merger/internal/models"
)

type job struct {
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

// httpClient is shared across downloads. The 10-minute timeout accommodates
// large GTFS zips (ÖBB, VOR, etc.) that can exceed 100 MB on slow uplinks
// from the DBP servers. 90 s was observed to be too short in production.
var httpClient = &http.Client{Timeout: 10 * time.Minute}

const maxAttempts = 3

// permanentError marks a failure that retrying cannot fix (e.g. HTTP 404).
type permanentError struct{ error }

// DownloadDataset unconditionally fetches a single GTFS zip into outDir.
// The body is streamed to "<file>.tmp" and renamed into place only once it
// was received completely, so a crash or dropped connection never leaves a
// truncated zip behind. Transient failures (network errors, 5xx, 429, a
// body cut off mid-stream, 401) are retried.
func DownloadDataset(a *auth.DBPAuth, datasetID, year, filename, outDir string) error {
	if err := os.MkdirAll(outDir, os.ModePerm); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	path := filepath.Join(outDir, filename)
	url := fmt.Sprintf("%s/api/public/v1/data-sets/%s/%s/file", auth.DBPBase, datasetID, year)

	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt-1) * 2 * time.Second)
		}
		if err = fetchToFile(a, url, path); err == nil {
			log.Printf("saved   %s", path)
			return nil
		}
		var perm permanentError
		if errors.As(err, &perm) {
			return err
		}
		log.Printf("retry   %s (attempt %d/%d): %v", filename, attempt, maxAttempts, err)
	}
	return fmt.Errorf("all %d attempts failed: %w", maxAttempts, err)
}

// fetchToFile performs one download attempt, writing atomically to path.
func fetchToFile(a *auth.DBPAuth, url, path string) error {
	headers, err := a.Header()
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return permanentError{fmt.Errorf("build request: %w", err)}
	}
	req.Header = headers

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized:
		a.InvalidateToken() // force a real refresh on the next attempt
		return errors.New("401 unauthorized, refreshing token")
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return fmt.Errorf("unexpected status %s", resp.Status)
	default:
		return permanentError{fmt.Errorf("unexpected status %s", resp.Status)}
	}

	tmp := path + ".tmp"
	n, err := writeFile(tmp, resp.Body)
	if err == nil && resp.ContentLength >= 0 && n != resp.ContentLength {
		err = fmt.Errorf("short download: got %d of %d bytes", n, resp.ContentLength)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return permanentError{err}
	}
	return nil
}

// writeFile streams r into path and returns the number of bytes written.
func writeFile(path string, r io.Reader) (int64, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("create file: %w", err)
	}
	n, err := io.Copy(f, r)
	if err != nil {
		f.Close()
		return n, fmt.Errorf("write file: %w", err)
	}
	return n, f.Close()
}

// DownloadResult is returned by DownloadAllDatasets.
type DownloadResult struct {
	// UpdatedManifest reflects the versions that are actually available
	// locally. A dataset whose download failed keeps its previous entry (or
	// none), so the next run retries it instead of treating it as current.
	UpdatedManifest models.Manifest
	// Changed is true if the upstream dataset versions differ from the
	// manifest passed in, i.e. a re-merge is warranted.
	Changed bool
	// NewFiles lists the filenames that were freshly downloaded.
	NewFiles []string
	// Failed lists the filenames that could not be downloaded.
	Failed []string
}

// DownloadAllDatasets compares the upstream GTFS dataset versions with
// manifest. If nothing changed and force is false, it returns without
// downloading anything. Otherwise it downloads every current dataset file
// that is missing from outDir — not just the changed ones — so that the
// directory always holds the complete set to merge, even on a fresh checkout
// without a cache.
func DownloadAllDatasets(username, password string, outDir string, maxWorkers int, verbose, force bool, manifest models.Manifest) (DownloadResult, error) {
	aut := auth.NewAuth(username, password)

	log.Printf("fetch   datasets from DBP...")
	datasets, err := auth.GetDatasets(aut)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("fetching datasets: %w", err)
	}

	remote := make(models.Manifest)
	var jobs []job
	for _, ds := range datasets {
		if !auth.IsGTFS(ds) || len(ds.ActiveVersions) == 0 {
			continue
		}
		if len(ds.ActiveVersions) > 1 {
			log.Printf("warn    dataset %s has %d active versions; using the first", ds.ID, len(ds.ActiveVersions))
		}
		v := ds.ActiveVersions[0]
		remote[ds.ID] = models.ManifestEntry{
			Year:         v.Year,
			OriginalName: v.DataSetVersion.File.OriginalName,
		}
		jobs = append(jobs, job{id: ds.ID, year: v.Year, name: v.DataSetVersion.File.OriginalName})
	}

	changed := !maps.Equal(remote, manifest)
	if !changed && !force {
		log.Printf("found   %d GTFS dataset(s), all unchanged", len(jobs))
		return DownloadResult{UpdatedManifest: remote}, nil
	}

	var todo []job
	for _, j := range jobs {
		if _, err := os.Stat(filepath.Join(outDir, j.name)); err == nil {
			if verbose {
				log.Printf("skip    %s (already on disk)", j.name)
			}
			continue
		}
		todo = append(todo, j)
	}
	log.Printf("found   %d GTFS dataset(s), downloading %d missing...", len(jobs), len(todo))

	var (
		wg      sync.WaitGroup
		jobChan = make(chan job)
		mu      sync.Mutex
		newFile []string
		failed  = make(map[string]bool) // dataset ID -> failed
		failedN []string
	)

	if maxWorkers < 1 {
		maxWorkers = 1
	}
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := range jobChan {
				if verbose {
					log.Printf("worker  %d downloading %s", workerID, j.name)
				}
				err := DownloadDataset(aut, j.id, j.year, j.name, outDir)
				mu.Lock()
				if err != nil {
					log.Printf("failed  %s: %v", j.name, err)
					failed[j.id] = true
					failedN = append(failedN, j.name)
				} else {
					newFile = append(newFile, j.name)
				}
				mu.Unlock()
			}
		}(i)
	}
	for _, j := range todo {
		jobChan <- j
	}
	close(jobChan)
	wg.Wait()

	// Only record versions we really have. Failed datasets fall back to the
	// previous entry so the next run notices the mismatch and retries.
	updated := make(models.Manifest, len(remote))
	for id, e := range remote {
		if !failed[id] {
			updated[id] = e
		} else if prev, ok := manifest[id]; ok {
			updated[id] = prev
		}
	}

	log.Printf("done    downloads complete (%d new, %d failed)", len(newFile), len(failedN))

	return DownloadResult{
		UpdatedManifest: updated,
		Changed:         changed,
		NewFiles:        newFile,
		Failed:          failedN,
	}, nil
}
