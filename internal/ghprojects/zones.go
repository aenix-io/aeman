package ghprojects

import (
	"strconv"
	"strings"
)

// parseFloat parses a decimal string into a float64.
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

// zoneColors maps each Ford zone onto the GitHub single-select option colours
// that represent it. It mirrors web/src/zones.ts.
var zoneColors = map[ZoneKey][]string{
	ZoneGray:   {"GRAY"},
	ZoneGreen:  {"GREEN"},
	ZoneYellow: {"YELLOW", "ORANGE"},
	ZoneRed:    {"RED", "PINK"},
}

// colorToZone is the reverse index of zoneColors.
var colorToZone = func() map[string]ZoneKey {
	m := make(map[string]ZoneKey)
	for zone, colors := range zoneColors {
		for _, c := range colors {
			m[strings.ToUpper(c)] = zone
		}
	}
	return m
}()

// zoneFromColor maps a GitHub single-select option colour onto a zone.
func zoneFromColor(color string) ZoneKey {
	if color == "" {
		return ""
	}
	return colorToZone[strings.ToUpper(color)]
}

// optionForZone finds the option id in a single-select field for a zone.
func optionForZone(field *ProjectField, zone ZoneKey) string {
	if field == nil {
		return ""
	}
	wanted := zoneColors[zone]
	for _, opt := range field.Options {
		for _, w := range wanted {
			if strings.EqualFold(opt.Color, w) {
				return opt.ID
			}
		}
	}
	return ""
}

// optionByName finds the option id in a single-select field by option name.
func optionByName(field *ProjectField, name string) string {
	if field == nil {
		return ""
	}
	for _, opt := range field.Options {
		if equalFold(opt.Name, name) {
			return opt.ID
		}
	}
	return ""
}

// equalFold reports whether a and b are equal under Unicode case-folding, after
// trimming surrounding whitespace.
func equalFold(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
