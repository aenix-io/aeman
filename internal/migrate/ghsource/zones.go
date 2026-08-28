package ghsource

import (
	"strings"

	"github.com/aenix-io/aeman/pkg/board"
)

// zoneColors maps each zone onto the GitHub single-select option colours that
// represent it. It mirrors web/src/zones.ts.
var zoneColors = map[board.ZoneKey][]string{
	board.ZoneGray:   {"GRAY"},
	board.ZoneGreen:  {"GREEN"},
	board.ZoneYellow: {"YELLOW", "ORANGE"},
	board.ZoneRed:    {"RED", "PINK"},
}

// colorToZone is the reverse index of zoneColors.
var colorToZone = func() map[string]board.ZoneKey {
	m := make(map[string]board.ZoneKey)
	for zone, colors := range zoneColors {
		for _, c := range colors {
			m[strings.ToUpper(c)] = zone
		}
	}
	return m
}()

// zoneFromColor maps a GitHub single-select option colour onto a zone.
func zoneFromColor(color string) board.ZoneKey {
	if color == "" {
		return board.ZoneNone
	}
	return colorToZone[strings.ToUpper(color)]
}
