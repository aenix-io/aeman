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

// DefaultSize is what a card nobody has sized WEIGHS on a board: M.
//
// Not zero. A board that weighs unsized work as nothing tells a person their
// week is empty while they are drowning in it, and the number goes up as the
// cards are sized — which reads as the sizing having caused the load. M is
// the middle of the board's own record: of 2194 sized cards on the
// production board 40% are S, 34% M, 23% L, 1% XL — the median card is M and
// the mean is 2.14 points — and assuming M costs less than any other guess
// (mean error 0.96 points against 1.14 for S and 1.97 for L). The cards most
// likely to be left unsized, the ones with no description at all, average
// 1.86 points, which is nearer M than S too.
const DefaultSize = SizeM

var points = map[SizeKey]int{SizeS: 1, SizeM: 2, SizeL: 4, SizeXL: 8}

// Points is the weight of a size on the scale: 1, 2, 4, 8, and 0 for the
// empty size. It is the SCALE, not what a card weighs — an unsized card
// weighs DefaultSize (PointsOf).
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
	sum, has := 0, false
	for _, k := range b.Cards {
		if k.Parent == c.ItemID {
			sum += weigh(k)
			has = true
		}
	}
	if has {
		return sum
	}
	return weigh(c)
}

// weigh is one card's own weight: its size, or — for an unsized one — what
// its KIND usually costs. Every sum a board draws goes through it, so
// "unsized" costs the same everywhere: in a person's load, in a week's plan
// and in the record a capacity is read off.
//
// A REVIEW card unsized weighs S rather than the default. A review is
// somebody reading finished work and saying yes or no; the sizing rubric
// calls it S by definition, and it is the one kind of card the board creates
// on its own, in bulk — one for every card sent to review. Weighing those as
// M put two points on a reviewer for each thing they were asked to look at,
// which on a board where nothing is sized is most of what their number was.
// A review somebody DID size keeps the size they gave it: the default is a
// guess about the usual, not a cap on the unusual.
func weigh(c Card) int {
	if c.Size != SizeNone {
		return Points(c.Size)
	}
	if c.ReviewOf != "" {
		return Points(SizeS)
	}
	return Points(DefaultSize)
}

// LoadNow is CarryingNow in points: the same cards a person is carrying today
// — theirs, open, not put off to a week ahead, subtasks riding their parent
// — weighed with PointsOf instead of counted. Like CarryingNow it ignores the
// filter on purpose: a person is not read through one, and the number beside
// their name has to be the whole of it. Unsized cards weigh the default, so
// the number is honest on a board nobody has sized yet.
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
