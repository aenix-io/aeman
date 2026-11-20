package apiserver

// The Plans reading of a listing (ADR 0002): what the Team, Me and Backlog
// boards draw in their Tasks | Plans switch's Plans, a listing asked with
// group=plans carries as `groups`, so an agent reads a day or a backlog by
// plan item the way a person does. The rule is web/src/grouping.ts's, on
// the shared fixture pkg/board/testdata/plan_groups.json: a card stands
// under its DIRECT plan item — the nearest entry of its lineage with a
// level (a review card's lineage is its task's, so it stands with it) —
// and the cards in no plan item under none ("Ungrouped", no `plan`); a
// subtask rides its parent and is in no group.

// PlanGroup is one group of a Plans reading: the plan item (absent for the
// cards in none), the cell it is drawn in on the board, and its cards by
// uid in the listing's order.
type PlanGroup struct {
	// Assignee is the Team board's column: the person, "" for nobody. A
	// card of several people stands in each of their columns.
	Assignee *string `json:"assignee,omitempty"`
	// Zone is the band, on the Team and Me boards.
	Zone  string      `json:"zone,omitempty"`
	Plan  *LineageRef `json:"plan,omitempty"`
	Cards []string    `json:"cards"`
}

// planZones is the order the day boards draw their bands in.
var planZones = []string{"urgent", "unplanned", "planned", "niceToHave"}

// PlanGroups is the Plans reading of a listing's cards for a view: per band
// on Me and per person and band on Team, each plan item gathered where its
// first card stands; on the Backlog the one list cut into runs of
// neighbouring cards of one plan item, so a plan item split by another
// stands in two runs. Nil for a view the boards draw no groups on.
func PlanGroups(items []Card, view string) []PlanGroup {
	var cards []Card
	for _, c := range items {
		if !c.Status.Subtask {
			cards = append(cards, c)
		}
	}
	switch view {
	case "me":
		out := []PlanGroup{}
		for _, z := range planZones {
			out = append(out, gathered(inZone(cards, z), nil, z)...)
		}
		return out
	case "team":
		var people []string
		seen := map[string]bool{}
		for _, c := range cards {
			for _, p := range columnsOf(c) {
				if !seen[p] {
					seen[p] = true
					people = append(people, p)
				}
			}
		}
		out := []PlanGroup{}
		for _, p := range people {
			var mine []Card
			for _, c := range cards {
				for _, q := range columnsOf(c) {
					if q == p {
						mine = append(mine, c)
						break
					}
				}
			}
			for _, z := range planZones {
				out = append(out, gathered(inZone(mine, z), &p, z)...)
			}
		}
		return out
	case "backlog":
		out := []PlanGroup{}
		for _, c := range cards {
			plan := planOf(c)
			if n := len(out); n > 0 && planUID(out[n-1].Plan) == planUID(plan) {
				out[n-1].Cards = append(out[n-1].Cards, c.Metadata.UID)
				continue
			}
			out = append(out, PlanGroup{Plan: plan, Cards: []string{c.Metadata.UID}})
		}
		return out
	}
	return nil
}

// gathered is one cell's groups: each plan item where its first card
// stands, its cards in the cell's order.
func gathered(cards []Card, assignee *string, zone string) []PlanGroup {
	var out []PlanGroup
	at := map[string]int{}
	for _, c := range cards {
		plan := planOf(c)
		key := planUID(plan)
		if i, ok := at[key]; ok {
			out[i].Cards = append(out[i].Cards, c.Metadata.UID)
			continue
		}
		at[key] = len(out)
		g := PlanGroup{Zone: zone, Plan: plan, Cards: []string{c.Metadata.UID}}
		if assignee != nil {
			p := *assignee
			g.Assignee = &p
		}
		out = append(out, g)
	}
	return out
}

// inZone is the cards of one band; a card with no band stands in the
// planned one, as the boards draw it.
func inZone(cards []Card, zone string) []Card {
	var out []Card
	for _, c := range cards {
		z := c.Spec.Zone
		if z == "" {
			z = "planned"
		}
		if z == zone {
			out = append(out, c)
		}
	}
	return out
}

// columnsOf is the Team board's columns a card stands in: each of its
// people, or nobody's.
func columnsOf(c Card) []string {
	if len(c.Spec.Assignees) == 0 {
		return []string{""}
	}
	return c.Spec.Assignees
}

// planOf is a card's direct plan item: the nearest lineage entry with a
// level; nil for a card in none.
func planOf(c Card) *LineageRef {
	for i := len(c.Status.Lineage) - 1; i >= 0; i-- {
		if c.Status.Lineage[i].Type != "" {
			ref := c.Status.Lineage[i]
			return &ref
		}
	}
	return nil
}

func planUID(p *LineageRef) string {
	if p == nil {
		return ""
	}
	return p.UID
}

// Regroup reads the groups of a listing that asked for them (group=plans)
// again from its items, after a caller has narrowed them (the MCP title
// filter): only the cards the board draws, and none the listing left out.
func (l *CardList) Regroup() {
	if l.drawn == nil {
		return
	}
	drawn := make([]Card, 0, len(l.Items))
	for _, c := range l.Items {
		if l.drawn[c.Metadata.UID] {
			drawn = append(drawn, c)
		}
	}
	l.Groups = PlanGroups(drawn, l.view)
}
