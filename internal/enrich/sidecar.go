package enrich

import (
	"compress/gzip"
	"encoding/csv"
	"os"
	"sort"
	"strconv"

	"github.com/PatrickSteil/austria-gtfs-merger/internal/osmfeat"
)

// sidecarRadiusM is the radius for the bicycle parking and elevator counts.
const sidecarRadiusM = 100.0

// SidecarRow holds the OSM attributes of one boarding stop. They have no
// standard GTFS field and are published next to the feed.
type SidecarRow struct {
	StopID                  string
	Tactile, Bench, Shelter string // yes|no|""
	BicycleParking100m      int
	Elevators100m           int
}

// featureGrid indexes features of the given kinds by grid cell.
type featureGrid struct {
	feats []osmfeat.Feature
	cells map[cell][]int
}

func newFeatureGrid(feats []osmfeat.Feature, keep func(osmfeat.Feature) bool) *featureGrid {
	g := &featureGrid{feats: feats, cells: make(map[cell][]int)}
	for i, f := range feats {
		if keep(f) {
			c := cellOf(f.Lat, f.Lon)
			g.cells[c] = append(g.cells[c], i)
		}
	}
	return g
}

// within calls fn for every indexed feature within radius meters of the point
// (radius must stay below one grid cell, about 150 m).
func (g *featureGrid) within(lat, lon, radius float64, fn func(f osmfeat.Feature, dist float64)) {
	c0 := cellOf(lat, lon)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			for _, i := range g.cells[cell{c0.y + dy, c0.x + dx}] {
				f := g.feats[i]
				if d := distM(lat, lon, f.Lat, f.Lon); d <= radius {
					fn(f, d)
				}
			}
		}
	}
}

// SidecarRows builds one row per matched boarding stop: the tags of its best
// OSM match plus the number of bicycle parking features and elevators within
// sidecarRadiusM. Stops without a match are left out. Rows are sorted by stop ID.
func SidecarRows(matches []StopMatch, feats []osmfeat.Feature) []SidecarRow {
	grid := newFeatureGrid(feats, func(f osmfeat.Feature) bool {
		return f.Kind == "bicycle_parking" || f.Kind == "elevator"
	})
	var rows []SidecarRow
	for _, m := range matches {
		if !m.HasBest {
			continue
		}
		row := SidecarRow{
			StopID: m.Stop.Id, Tactile: m.Best.f.Tactile, Bench: m.Best.f.Bench, Shelter: m.Best.f.Shelter,
		}
		grid.within(float64(m.Stop.Lat), float64(m.Stop.Lon), sidecarRadiusM, func(f osmfeat.Feature, _ float64) {
			if f.Kind == "bicycle_parking" {
				row.BicycleParking100m++
			} else {
				row.Elevators100m++
			}
		})
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].StopID < rows[j].StopID })
	return rows
}

// WriteSidecar writes the rows as gzip-compressed CSV.
func WriteSidecar(path string, rows []SidecarRow) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(f)
	cw := csv.NewWriter(zw)
	cw.Write([]string{"stop_id", "tactile_paving", "bench", "shelter", "bicycle_parking_100m", "elevators_100m"})
	for _, r := range rows {
		cw.Write([]string{r.StopID, r.Tactile, r.Bench, r.Shelter,
			strconv.Itoa(r.BicycleParking100m), strconv.Itoa(r.Elevators100m)})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		f.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
