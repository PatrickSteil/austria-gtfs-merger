package download

import (
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/auth"
	"github.com/PatrickSteil/austria-gtfs-merger/internal/models"
)

// timetableYear returns the timetable year in force on day t (UTC).
//
// Austrian timetable years run from the Sunday after the second Saturday of
// December until the same day a year later, and carry the year in which they
// end: the "2026" feeds cover 2025-12-14 to 2026-12-12 and the "2027" feeds
// start on 2026-12-13. DBP publishes the next year's data weeks ahead and then
// stops listing the current year as active, so the active version alone is not
// the one in force.
func timetableYear(t time.Time) int {
	t = t.UTC()
	if !t.Before(changeover(t.Year())) {
		return t.Year() + 1
	}
	return t.Year()
}

// changeover is the first day of the timetable year ending in year+1: the
// Sunday after the second Saturday of December of the given year.
func changeover(year int) time.Time {
	d := time.Date(year, time.December, 1, 0, 0, 0, 0, time.UTC)
	firstSaturday := 1 + (int(time.Saturday)-int(d.Weekday())+7)%7
	return time.Date(year, time.December, firstSaturday+7+1, 0, 0, 0, 0, time.UTC)
}

// probeFunc returns the file name DBP serves for a dataset and year, or an
// error if that year is not available.
type probeFunc func(datasetID, year string) (string, error)

// selectVersions picks, for every GTFS dataset, the version of the timetable
// year in force. It uses the active version when that already is the wanted
// year and otherwise asks probe for the wanted year. If the wanted year is not
// available (e.g. not published yet) it falls back to the active version.
func selectVersions(datasets []models.Dataset, wantedYear int, probe probeFunc) (models.Manifest, []job) {
	want := strconv.Itoa(wantedYear)
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
		year, name := v.Year, v.DataSetVersion.File.OriginalName

		if year != want {
			if wantedName, err := probe(ds.ID, want); err == nil {
				log.Printf("select  dataset %s: using %s instead of active %s (timetable year %s is in force)", ds.ID, want, year, want)
				year, name = want, wantedName
			} else {
				log.Printf("warn    dataset %s: no %s version (%v); using active %s", ds.ID, want, err, year)
			}
		}

		remote[ds.ID] = models.ManifestEntry{Year: year, OriginalName: name}
		jobs = append(jobs, job{id: ds.ID, year: year, name: name})
	}
	return remote, jobs
}

// filenameFromDisposition extracts the file name of a Content-Disposition
// header, rejecting anything that is not a plain base name.
func filenameFromDisposition(cd string) (string, error) {
	_, params, err := mime.ParseMediaType(cd)
	if err != nil {
		return "", fmt.Errorf("content-disposition %q: %w", cd, err)
	}
	name := params["filename"]
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) || strings.ContainsRune(name, '\\') {
		return "", fmt.Errorf("content-disposition %q has no usable filename", cd)
	}
	return name, nil
}

// ProbeVersion asks DBP which file it serves for a dataset and year without
// downloading it: only the response headers are read.
func ProbeVersion(a *auth.DBPAuth, datasetID, year string) (string, error) {
	url := fmt.Sprintf("%s/api/public/v1/data-sets/%s/%s/file", auth.DBPBase, datasetID, year)

	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt-1) * 2 * time.Second)
		}
		var name string
		if name, err = probeOnce(a, url); err == nil {
			return name, nil
		}
		var perm permanentError
		if errors.As(err, &perm) {
			return "", err
		}
	}
	return "", err
}

func probeOnce(a *auth.DBPAuth, url string) (string, error) {
	headers, err := a.Header()
	if err != nil {
		return "", fmt.Errorf("auth: %w", err)
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", permanentError{err}
	}
	req.Header = headers

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	// Closing without reading the body abandons the transfer.
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		name, err := filenameFromDisposition(resp.Header.Get("Content-Disposition"))
		if err != nil {
			return "", permanentError{err}
		}
		return name, nil
	case resp.StatusCode == http.StatusUnauthorized:
		a.InvalidateToken()
		return "", errors.New("401 unauthorized, refreshing token")
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return "", fmt.Errorf("unexpected status %s", resp.Status)
	default:
		return "", permanentError{fmt.Errorf("unexpected status %s", resp.Status)}
	}
}
