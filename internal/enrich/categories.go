package enrich

import (
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Category is the product category of a trip, taken from the leading letters
// of trip_short_name ("RJX 662" -> RJX).
type Category string

// CategoryOf extracts the category from a trip_short_name. Empty or purely
// numeric names have none.
func CategoryOf(tripShortName string) (Category, bool) {
	s := strings.TrimSpace(tripShortName)
	end := 0
	for i, r := range s {
		if !unicode.IsLetter(r) {
			break
		}
		end = i + len(string(r))
	}
	if end == 0 {
		return "", false
	}
	return Category(strings.ToUpper(s[:end])), true
}

// HVT (extended GTFS) route types used for Austrian rail products.
var railTypes = map[Category]int16{
	"RJ": 101, "RJX": 101, "ICE": 101,
	"IC": 102, "EC": 102, "D": 102, "WB": 102,
	"IR": 103,
	"NJ": 105, "EN": 105,
	"R": 106, "REX": 106, "CJX": 106,
	"SLB": 106, "PLB": 106, "ATB": 106, "SP": 106, "ER": 106, "RR": 106, "RB": 106, "OS": 106,
	"S": 109,
}

// ExtendedType returns the (extended) route_type for a route whose basic type
// is base and whose dominant trip category is c. Unknown categories keep base.
// Replacement buses are detected by name elsewhere (isReplacementBus).
func ExtendedType(c Category, base int16) int16 {
	switch base {
	case 2:
		if t, ok := railTypes[c]; ok {
			return t
		}
	case 7:
		switch c {
		case "SB", "LIF":
			return 6
		case "FUN":
			return 7
		}
	}
	return base
}

var displayNames = map[Category]string{
	"RJX": "Railjet Xpress", "RJ": "Railjet", "ICE": "ICE", "IC": "InterCity", "EC": "EuroCity",
	"IR": "InterRegio", "NJ": "Nightjet", "EN": "EuroNight", "D": "Schnellzug", "WB": "WESTbahn",
	"REX": "Regionalexpress", "R": "Regionalzug", "CJX": "CityJet Xpress", "S": "S-Bahn",
	"SB": "Seilbahn", "LIF": "Lift", "FUN": "Standseilbahn",
}

// DisplayName is a human readable product name for route_desc. Unknown
// categories are returned as they are.
func DisplayName(c Category) string {
	if n, ok := displayNames[c]; ok {
		return n
	}
	return string(c)
}

var categoryColors = map[Category]string{
	"RJ": "C8102E", "RJX": "C8102E", "ICE": "C8102E",
	"IC": "8B1A1A", "EC": "8B1A1A", "IR": "8B1A1A",
	"NJ": "1F2A5C", "EN": "1F2A5C",
	"R": "5B6770", "REX": "5B6770", "CJX": "5B6770",
	"S": "0067B1",
}

// ColorFor returns the palette color (hex without '#') of a rail category.
func ColorFor(c Category) (string, bool) {
	col, ok := categoryColors[c]
	return col, ok
}

func luminance(hex string) (float64, bool) {
	if len(hex) != 6 {
		return 0, false
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, false
	}
	lin := func(b uint64) float64 {
		c := float64(b) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(v>>16&0xff) + 0.7152*lin(v>>8&0xff) + 0.0722*lin(v&0xff), true
}

// TextColorFor returns "000000" or "FFFFFF", whichever contrasts better with
// the background color bgHex. Invalid input yields "000000".
func TextColorFor(bgHex string) string {
	l, ok := luminance(bgHex)
	if !ok {
		return "000000"
	}
	// Contrast with white is 1.05/(l+0.05), with black (l+0.05)/0.05.
	if 1.05/(l+0.05) > (l+0.05)/0.05 {
		return "FFFFFF"
	}
	return "000000"
}
