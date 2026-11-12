package download

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/models"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
}

func TestChangeoverMatchesFeedBoundaries(t *testing.T) {
	// first day of the next timetable year, as seen in the DBP feeds
	// (2026 feeds start 2025-12-14, 2027 feeds start 2026-12-13)
	for year, want := range map[int]int{2025: 14, 2026: 13, 2027: 12, 2028: 10} {
		if got := changeover(year); got.Day() != want || got.Month() != time.December || got.Weekday() != time.Sunday {
			t.Errorf("changeover(%d) = %v, want a Sunday, December %d", year, got, want)
		}
	}
}

func TestTimetableYear(t *testing.T) {
	cases := []struct {
		at   time.Time
		want int
	}{
		{day(2025, 12, 13), 2025},
		{day(2025, 12, 14), 2026},
		{day(2026, 1, 1), 2026},
		{day(2026, 11, 12), 2026},
		{day(2026, 12, 12), 2026},
		{day(2026, 12, 13), 2027},
		{day(2026, 12, 31), 2027},
		{day(2027, 6, 1), 2027},
	}
	for _, c := range cases {
		if got := timetableYear(c.at); got != c.want {
			t.Errorf("timetableYear(%s) = %d, want %d", c.at.Format("2006-01-02"), got, c.want)
		}
	}
}

func dataset(t *testing.T, id, year, file string) models.Dataset {
	t.Helper()
	raw := `{"id":"` + id + `","nameEn":"x","tags":[{"valueEn":"GTFS"}],
		"activeVersions":[{"year":"` + year + `","dataSetVersion":{"file":{"originalName":"` + file + `"}}}]}`
	var ds models.Dataset
	if err := json.Unmarshal([]byte(raw), &ds); err != nil {
		t.Fatal(err)
	}
	return ds
}

func TestSelectVersions(t *testing.T) {
	datasets := []models.Dataset{
		dataset(t, "58", "2027", "a_2027.zip"), // rolled over, 2026 still served
		dataset(t, "69", "2026", "b_2026.zip"), // already the wanted year
		dataset(t, "72", "2027", "c_2027.zip"), // wanted year not served
	}
	var probed []string
	probe := func(id, year string) (string, error) {
		probed = append(probed, id+"/"+year)
		if id == "72" {
			return "", errors.New("500")
		}
		return "fresh_" + id + "_" + year + ".zip", nil
	}

	remote, jobs := selectVersions(datasets, 2026, probe)

	want := models.Manifest{
		"58": {Year: "2026", OriginalName: "fresh_58_2026.zip"},
		"69": {Year: "2026", OriginalName: "b_2026.zip"},
		"72": {Year: "2027", OriginalName: "c_2027.zip"},
	}
	if len(remote) != len(want) {
		t.Fatalf("remote = %v, want %v", remote, want)
	}
	for id, e := range want {
		if remote[id] != e {
			t.Errorf("remote[%s] = %v, want %v", id, remote[id], e)
		}
	}
	if len(jobs) != 3 {
		t.Fatalf("got %d jobs, want 3", len(jobs))
	}
	for _, j := range jobs {
		if e := remote[j.id]; j.year != e.Year || j.name != e.OriginalName {
			t.Errorf("job %+v does not match manifest entry %+v", j, e)
		}
	}
	for _, p := range probed {
		if p == "69/2026" {
			t.Errorf("probed dataset 69 although the active version already is the wanted year")
		}
	}
}

func TestSelectVersionsSkipsNonGTFS(t *testing.T) {
	ds := dataset(t, "1", "2026", "x.zip")
	ds.Tags = nil
	remote, jobs := selectVersions([]models.Dataset{ds}, 2026, func(string, string) (string, error) {
		t.Fatal("probe must not be called")
		return "", nil
	})
	if len(remote) != 0 || len(jobs) != 0 {
		t.Errorf("expected nothing selected, got %v %v", remote, jobs)
	}
}

func TestFilenameFromDisposition(t *testing.T) {
	ok, err := filenameFromDisposition(`attachment; filename=20261112-0332_gtfs_vor_2026.zip`)
	if err != nil || ok != "20261112-0332_gtfs_vor_2026.zip" {
		t.Errorf("got %q, %v", ok, err)
	}
	for _, bad := range []string{``, `attachment`, `attachment; filename=../evil.zip`, `attachment; filename="a/b.zip"`, `attachment; filename=..`} {
		if name, err := filenameFromDisposition(bad); err == nil {
			t.Errorf("filenameFromDisposition(%q) = %q, want error", bad, name)
		}
	}
}
