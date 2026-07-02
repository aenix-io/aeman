package ghprojects

import "testing"

func TestZoneFromColor(t *testing.T) {
	cases := map[string]ZoneKey{
		"GRAY":    ZoneGray,
		"green":   ZoneGreen,
		"YELLOW":  ZoneYellow,
		"orange":  ZoneYellow,
		"RED":     ZoneRed,
		"PINK":    ZoneRed,
		"BLUE":    "",
		"":        "",
		"unknown": "",
	}
	for color, want := range cases {
		if got := zoneFromColor(color); got != want {
			t.Errorf("zoneFromColor(%q) = %q, want %q", color, got, want)
		}
	}
}

func TestOptionForZoneAndByName(t *testing.T) {
	field := &ProjectField{
		ID:   "F",
		Name: "Zone",
		Options: []SingleSelectOption{
			{ID: "o_gray", Name: "Planned", Color: "GRAY"},
			{ID: "o_red", Name: "Critical", Color: "RED"},
		},
	}
	if got := optionForZone(field, ZoneRed); got != "o_red" {
		t.Errorf("optionForZone(red) = %q, want o_red", got)
	}
	if got := optionForZone(field, ZoneGreen); got != "" {
		t.Errorf("optionForZone(green) = %q, want empty", got)
	}
	if got := optionByName(field, "planned"); got != "o_gray" {
		t.Errorf("optionByName(planned) = %q, want o_gray", got)
	}
	if got := optionByName(field, "missing"); got != "" {
		t.Errorf("optionByName(missing) = %q, want empty", got)
	}
}

func TestRolesByName(t *testing.T) {
	board := &Board{Fields: []ProjectField{
		{ID: "1", Name: "Zone"},
		{ID: "2", Name: "Readiness"},
		{ID: "3", Name: "Due date"},
		{ID: "4", Name: "Iteration"},
		{ID: "5", Name: "Stage"},
		{ID: "6", Name: "Team"},
		{ID: "7", Name: "Unrelated"},
	}}
	r := board.roles()
	checks := []struct {
		got  *ProjectField
		want string
	}{
		{r.Zone, "1"}, {r.Progress, "2"}, {r.Day, "3"},
		{r.Sprint, "4"}, {r.Status, "5"}, {r.Team, "6"},
	}
	for i, c := range checks {
		if c.got == nil || c.got.ID != c.want {
			t.Errorf("role %d resolved to %v, want id %s", i, c.got, c.want)
		}
	}
}

func TestParseNotesFromDraftBody(t *testing.T) {
	content := &rawContent{Body: "intro line\n- [2026-06-21T09:00:00Z] first\n* [2026-06-22T09:00:00Z] second"}
	notes := parseNotes(content, "I_1")
	if len(notes) != 2 {
		t.Fatalf("got %d notes, want 2", len(notes))
	}
	if notes[0].Body != "first" || notes[0].CreatedAt != "2026-06-21T09:00:00Z" || notes[0].Source != "draft" {
		t.Errorf("note[0] = %+v", notes[0])
	}
}

func TestParseNotesPrefersComments(t *testing.T) {
	content := &rawContent{
		Body: "- [2026-06-21T09:00:00Z] ignored",
		Comments: &struct {
			Nodes []rawComment `json:"nodes"`
		}{Nodes: []rawComment{{ID: "C1", Body: "hi", CreatedAt: "2026-06-20T10:00:00Z", Author: &struct {
			Login string `json:"login"`
		}{Login: "octocat"}}}},
	}
	notes := parseNotes(content, "I_1")
	if len(notes) != 1 || notes[0].Source != "comment" || notes[0].Author != "octocat" {
		t.Fatalf("notes = %+v", notes)
	}
}
