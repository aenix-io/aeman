package board

import "strings"

// SizeKey is what somebody said a card weighs: S, M, L or XL. It mirrors the
// SizeKey union in web/src/providers/types.ts; SizeNone ("") is unsized.
//
// The LETTER is what is stored and what a person picks on a daily sync; the
// points are derived (Points), never written, so the scale can be retuned
// without touching a card. The scale doubles at each step so a week of S-work
// and a week of L-work can be compared at all: S is up to a couple of hours,
// M half a day to a day, L two to five days, XL a week or more.
type SizeKey string

// The four sizes, mirroring web/src/size.ts.
const (
	SizeNone SizeKey = ""
	SizeS    SizeKey = "S"
	SizeM    SizeKey = "M"
	SizeL    SizeKey = "L"
	SizeXL   SizeKey = "XL"
)

// SizeOrder is the four sizes smallest first — the order a picker shows them.
var SizeOrder = []SizeKey{SizeS, SizeM, SizeL, SizeXL}

var points = map[SizeKey]int{SizeS: 1, SizeM: 2, SizeL: 4, SizeXL: 8}

// Points is the weight of a size: 1, 2, 4, 8 — and 0 for a card nobody
// sized, which is honest: the board cannot say what it does not know.
func Points(s SizeKey) int { return points[s] }

// ParseSize reads a size as people and tools type it — any case, stray
// spaces — and refuses anything that is not one of the four: a size nothing
// knows would weigh nothing and read as unsized, silently. The empty string
// is SizeNone and is accepted: it is how a size is cleared.
func ParseSize(raw string) (SizeKey, bool) {
	switch SizeKey(strings.ToUpper(strings.TrimSpace(raw))) {
	case SizeNone:
		return SizeNone, true
	case SizeS:
		return SizeS, true
	case SizeM:
		return SizeM, true
	case SizeL:
		return SizeL, true
	case SizeXL:
		return SizeXL, true
	}
	return SizeNone, false
}

// PointsOf is what a card WEIGHS on a board: its own size, or — when it has
// subtasks somebody has sized — the sum of theirs. This is the umbrella rule,
// and it is dynamic on purpose: umbrellas are not born as umbrellas (on the
// production history forty percent get their first child three days or more
// after creation, and nothing in a card's text predicts which will), so the
// parent's own estimate stands until the children exist and is replaced by
// their sum once they do — the way a parent's progress already derives from
// its subtasks. The total never counts twice, and a card split a minute ago
// into unsized pieces still weighs what its author said.
func PointsOf(b Board, c Card) int {
	sum, sized := 0, false
	for _, k := range b.Cards {
		if k.Parent == c.ItemID && k.Size != SizeNone {
			sum += Points(k.Size)
			sized = true
		}
	}
	if sized {
		return sum
	}
	return Points(c.Size)
}

// LoadNow is CarryingNow in points: the same cards a person is carrying today
// — theirs, open, not put off to a week ahead, subtasks riding their parent
// — weighed with PointsOf instead of counted. Like CarryingNow it ignores the
// filter on purpose: a person is not read through one, and the number beside
// their name has to be the whole of it.
func LoadNow(b Board, today string) map[string]int {
	out := map[string]int{}
	for _, c := range b.Cards {
		if !carriedNow(c, today) {
			continue
		}
		out[c.Assignees[0]] += PointsOf(b, c)
	}
	return out
}
