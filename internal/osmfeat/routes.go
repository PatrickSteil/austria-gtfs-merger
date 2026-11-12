package osmfeat

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Point is a coordinate.
type Point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// RouteRel is an OSM public transport route relation reduced to what the
// matching to GTFS routes needs.
type RouteRel struct {
	ID       int64   `json:"id"`
	Mode     string  `json:"mode"` // bus|rail|tram|subway|ferry|aerialway
	Ref      string  `json:"ref,omitempty"`
	Name     string  `json:"name,omitempty"`
	From     string  `json:"from,omitempty"`
	To       string  `json:"to,omitempty"`
	Network  string  `json:"network,omitempty"`
	Operator string  `json:"operator,omitempty"`
	Colour   string  `json:"colour,omitempty"` // RRGGBB
	Stops    []Point `json:"stops,omitempty"`  // stop and platform members
}

// ModeOfRoute maps an OSM route=* value to a transport mode.
func ModeOfRoute(routeTag string) (string, bool) {
	switch routeTag {
	case "bus", "trolleybus":
		return "bus", true
	case "train", "light_rail":
		return "rail", true
	case "tram":
		return "tram", true
	case "subway":
		return "subway", true
	case "ferry":
		return "ferry", true
	case "funicular":
		return "aerialway", true
	}
	return "", false
}

// NormalizeColour accepts RRGGBB, #RRGGBB and #RGB and returns upper case
// RRGGBB. Anything else, including color names, yields "".
func NormalizeColour(v string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "#")
	if len(v) == 3 {
		v = string([]byte{v[0], v[0], v[1], v[1], v[2], v[2]})
	}
	if len(v) != 6 {
		return ""
	}
	if _, err := strconv.ParseUint(v, 16, 32); err != nil {
		return ""
	}
	return strings.ToUpper(v)
}

// RouteRelFromTags builds a RouteRel from a relation's tags, or reports false
// if it is not a usable public transport route.
func RouteRelFromTags(id int64, tags map[string]string) (RouteRel, bool) {
	if tags["type"] != "route" {
		return RouteRel{}, false
	}
	mode, ok := ModeOfRoute(tags["route"])
	if !ok || (tags["ref"] == "" && tags["name"] == "") {
		return RouteRel{}, false
	}
	return RouteRel{
		ID: id, Mode: mode, Ref: tags["ref"], Name: tags["name"], From: tags["from"], To: tags["to"],
		Network: tags["network"], Operator: tags["operator"], Colour: NormalizeColour(tags["colour"]),
	}, true
}

type routeFile struct {
	Version int        `json:"version"`
	Routes  []RouteRel `json:"routes"`
}

// WriteRoutes stores the relations as gzip-compressed JSON, sorted by ID.
func WriteRoutes(path string, rels []RouteRel) error {
	sorted := append([]RouteRel(nil), rels...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	return writeGzipJSON(path, routeFile{Version: FormatVersion, Routes: sorted})
}

// ReadRoutes loads a file written by WriteRoutes. Other versions yield ErrFormat.
func ReadRoutes(path string) ([]RouteRel, error) {
	var rf routeFile
	if err := readGzipJSON(path, &rf); err != nil {
		return nil, err
	}
	if rf.Version != FormatVersion {
		return nil, fmt.Errorf("%w: version %d, want %d", ErrFormat, rf.Version, FormatVersion)
	}
	return rf.Routes, nil
}
