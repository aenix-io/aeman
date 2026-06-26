package ghprojects

import "strings"

// roleAliases maps a field role to the field names (case-insensitive) that fill
// it. It mirrors web/src/providers/fields.ts, extended with a "team" role.
var roleAliases = map[string][]string{
	"zone":     {"zone", "priority zone", "зона"},
	"progress": {"progress", "readiness", "% done", "percent", "готовность"},
	"day":      {"day", "date", "due date", "due", "день", "дата"},
	"sprint":   {"sprint", "iteration", "спринт", "итерация"},
	"status":   {"status", "stage", "статус"},
	"team":     {"team", "group", "команда", "группа"},
}

// roles maps the board's fields onto well-known roles by name.
func (b *Board) roles() FieldRoles {
	var r FieldRoles
	for i := range b.Fields {
		f := &b.Fields[i]
		name := strings.ToLower(strings.TrimSpace(f.Name))
		switch {
		case r.Zone == nil && matchesAlias("zone", name):
			r.Zone = f
		case r.Progress == nil && matchesAlias("progress", name):
			r.Progress = f
		case r.Day == nil && matchesAlias("day", name):
			r.Day = f
		case r.Sprint == nil && matchesAlias("sprint", name):
			r.Sprint = f
		case r.Status == nil && matchesAlias("status", name):
			r.Status = f
		case r.Team == nil && matchesAlias("team", name):
			r.Team = f
		}
	}
	return r
}

// matchesAlias reports whether the lower-cased field name fills the given role.
func matchesAlias(role, lowerName string) bool {
	for _, alias := range roleAliases[role] {
		if alias == lowerName {
			return true
		}
	}
	return false
}
