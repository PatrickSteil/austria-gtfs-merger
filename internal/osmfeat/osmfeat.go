// Package osmfeat is the intermediate format between the OSM extraction
// (cmd/osm-extract) and the wheelchair matching step: a small list of
// public transport features with their accessibility tag.
package osmfeat

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"sort"
	"strings"
)

// Feature is one OSM public transport object reduced to what matching needs.
type Feature struct {
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	Kind       string  `json:"kind"`                 // bus|rail|tram|subway|ferry|aerialway
	Wheelchair string  `json:"wheelchair,omitempty"` // yes|no|limited|""
	Name       string  `json:"name,omitempty"`
	Ref        string  `json:"ref,omitempty"`
	Area       bool    `json:"area,omitempty"` // way/polygon (centroid), larger match radius
}

// ClassifyTags returns the transport mode of an OSM object and whether it is
// a relevant public transport feature at all.
func ClassifyTags(tags map[string]string) (string, bool) {
	switch {
	case tags["highway"] == "bus_stop", tags["amenity"] == "bus_station":
		return "bus", true
	case tags["amenity"] == "ferry_terminal":
		return "ferry", true
	case tags["aerialway"] == "station":
		return "aerialway", true
	case tags["railway"] == "tram_stop":
		return "tram", true
	case tags["railway"] == "subway_entrance":
		return "subway", true
	case tags["railway"] == "platform", tags["railway"] == "halt", tags["railway"] == "station":
		switch {
		case tags["tram"] == "yes":
			return "tram", true
		case tags["subway"] == "yes", tags["station"] == "subway":
			return "subway", true
		}
		return "rail", true
	}
	switch tags["public_transport"] {
	case "platform", "stop_position", "station":
		switch {
		case tags["bus"] == "yes":
			return "bus", true
		case tags["train"] == "yes", tags["light_rail"] == "yes":
			return "rail", true
		case tags["tram"] == "yes":
			return "tram", true
		case tags["subway"] == "yes":
			return "subway", true
		case tags["ferry"] == "yes":
			return "ferry", true
		}
	}
	return "", false
}

// NormalizeWheelchair maps an OSM wheelchair=* value to yes, no, limited or "".
func NormalizeWheelchair(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "yes", "designated":
		return "yes"
	case "no":
		return "no"
	case "limited":
		return "limited"
	}
	return ""
}

// Write stores the features as gzip-compressed JSON, sorted by latitude,
// longitude and name so the file is reproducible.
func Write(path string, fs []Feature) error {
	sorted := append([]Feature(nil), fs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Lat != b.Lat {
			return a.Lat < b.Lat
		}
		if a.Lon != b.Lon {
			return a.Lon < b.Lon
		}
		return a.Name < b.Name
	})
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(f)
	if err := json.NewEncoder(zw).Encode(sorted); err != nil {
		f.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Read loads a file written by Write.
func Read(path string) ([]Feature, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var fs []Feature
	if err := json.NewDecoder(zr).Decode(&fs); err != nil {
		return nil, err
	}
	return fs, nil
}
