package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckStats(t *testing.T) {
	lim := Limits{MinAgencies: 50, MinTrips: 500000, MinRailRoutes: 200, MaxDropPct: 20}
	good := Stats{Agencies: 132, Stops: 106000, Routes: 3457, Trips: 920000, RailRoutes: 600}

	if p := checkStats(good, nil, lim); len(p) != 0 {
		t.Errorf("good feed flagged: %v", p)
	}
	// The broken VOR-only release: few agencies, almost no rail.
	vorOnly := Stats{Agencies: 27, Stops: 40000, Routes: 929, Trips: 300000, RailRoutes: 14}
	p := checkStats(vorOnly, nil, lim)
	if len(p) != 3 {
		t.Errorf("want agencies, trips and rail violations, got %v", p)
	}
	// Drop relative to previous run.
	shrunk := good
	shrunk.Trips = 700000 // -24%
	if p := checkStats(shrunk, &good, lim); len(p) != 1 || !strings.Contains(p[0], "trips") {
		t.Errorf("want a single trips drop, got %v", p)
	}
	ok := good
	ok.Trips = 800000 // -13%
	if p := checkStats(ok, &good, lim); len(p) != 0 {
		t.Errorf("small drop flagged: %v", p)
	}
	// Zero limits disable the checks.
	if p := checkStats(Stats{}, &good, Limits{}); len(p) != 0 {
		t.Errorf("disabled checks flagged: %v", p)
	}
}

func TestStatsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	if s, err := loadStats(path); s != nil || err != nil {
		t.Fatalf("missing file: got %v, %v", s, err)
	}
	want := Stats{Agencies: 1, Stops: 2, Routes: 3, Trips: 4, RailRoutes: 5}
	if err := saveStats(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadStats(path)
	if err != nil || got == nil || *got != want {
		t.Fatalf("got %v, %v", got, err)
	}
}
