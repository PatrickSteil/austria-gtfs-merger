// Package osmfeat is the intermediate format between the OSM extraction
// (cmd/osm-extract) and the wheelchair matching step: a small list of
// public transport features with their accessibility tag.
package osmfeat

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Feature is one OSM public transport object reduced to what matching needs.
type Feature struct {
	ID   int64   `json:"id,omitempty"` // OSM object ID
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Kind string  `json:"kind"` // bus|rail|tram|subway|ferry|aerialway|entrance|elevator|bicycle_parking
	// Wheelchair is yes|no|limited|"".
	Wheelchair string `json:"wheelchair,omitempty"`
	Name       string `json:"name,omitempty"`
	Ref        string `json:"ref,omitempty"`      // used to match platform_code
	Platform   string `json:"platform,omitempty"` // valid platform designation, see PlatformRef
	// Tactile, Bench and Shelter are yes|no|"".
	Tactile string `json:"tactile_paving,omitempty"`
	Bench   string `json:"bench,omitempty"`
	Shelter string `json:"shelter,omitempty"`
	StopPos bool   `json:"stop_position,omitempty"` // public_transport=stop_position node
	Area    bool   `json:"area,omitempty"`          // way/polygon (centroid), larger match radius
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

// FormatVersion is the version of the files written by this package. Readers
// reject other versions so an outdated cache is never used with newer code.
const FormatVersion = 2

// ErrFormat is returned when a file has an unsupported format or version.
var ErrFormat = errors.New("osmfeat: unsupported file format")

type featureFile struct {
	Version  int       `json:"version"`
	Features []Feature `json:"features"`
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
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	return writeGzipJSON(path, featureFile{Version: FormatVersion, Features: sorted})
}

// Read loads a file written by Write. Other versions yield ErrFormat.
func Read(path string) ([]Feature, error) {
	var ff featureFile
	if err := readGzipJSON(path, &ff); err != nil {
		return nil, err
	}
	if ff.Version != FormatVersion {
		return nil, fmt.Errorf("%w: version %d, want %d", ErrFormat, ff.Version, FormatVersion)
	}
	return ff.Features, nil
}

func writeGzipJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(f)
	if err := json.NewEncoder(zw).Encode(v); err != nil {
		f.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func readGzipJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := json.NewDecoder(zr).Decode(v); err != nil {
		var ute *json.UnmarshalTypeError
		if errors.As(err, &ute) {
			return fmt.Errorf("%w: %v", ErrFormat, err)
		}
		return err
	}
	return nil
}

var platformRefRe = regexp.MustCompile(`^[0-9A-Za-z]{1,4}$`)

// PlatformRef returns the platform designation of an OSM object, or "" if it
// has none that is usable as platform_code. For buses only local_ref counts
// (a bus stop's ref is usually an operator number, not a bay).
func PlatformRef(kind string, tags map[string]string) string {
	candidates := []string{tags["local_ref"]}
	if kind != "bus" {
		candidates = append(candidates, tags["ref"])
	}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if platformRefRe.MatchString(c) {
			return c
		}
	}
	return ""
}

// NormalizeTri maps an OSM yes/no tag to "yes", "no" or "" (anything else).
func NormalizeTri(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "yes", "designated":
		return "yes"
	case "no":
		return "no"
	}
	return ""
}

// ClassifyExtra recognises the objects that are not transport stops but are
// used for stations and stop attributes.
func ClassifyExtra(tags map[string]string) (string, bool) {
	switch {
	case tags["amenity"] == "bicycle_parking":
		return "bicycle_parking", true
	case tags["highway"] == "elevator":
		return "elevator", true
	case tags["railway"] == "subway_entrance":
		return "entrance", true
	case tags["entrance"] != "" && (tags["railway"] != "" || tags["public_transport"] != ""):
		return "entrance", true
	}
	return "", false
}
