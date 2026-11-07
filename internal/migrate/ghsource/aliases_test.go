package ghsource

import "testing"

// A Projects v2 board only ever had an "Epic" field — the column was called
// that before schema 2 — so the migration has to recognise it as the scope
// role, or every project card would come in as a columnless task when it is
// to be left out (H15). The current name is taken too, for a board somebody
// renamed.
func TestAnEpicFieldFillsTheScopeRole(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Epic", "epic", "Scope", " scope "} {
		r := domainRoles([]Field{{ID: "f1", Name: name, DataType: "TEXT"}})
		if r.Scope == nil || r.Scope.ID != "f1" {
			t.Errorf("a %q field does not fill the scope role: %+v", name, r.Scope)
		}
	}
	if r := domainRoles([]Field{{ID: "f2", Name: "Epics owner", DataType: "TEXT"}}); r.Scope != nil {
		t.Errorf("a field merely containing the word is not the column: %+v", r.Scope)
	}
}
