// Package migrate moves a GitHub Projects v2 board into the git layout: the
// snapshot is the truth, the event log becomes annotation commits, every id
// is derived from its source so a re-run is byte-identical, and the report
// names everything that did not carry over. One way, idempotent, dry-runnable.
package migrate

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/aenix-io/aeman/internal/migrate/ghsource"
	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// Source is where the board is read from — *ghsource.Client in production,
// a fake in tests.
type Source interface {
	LoadBoard(ctx context.Context, owner string, project int) (ghsource.Export, error)
}

var _ Source = (*ghsource.Client)(nil)

// Options configures one migration.
type Options struct {
	Owner     string
	Board     int
	Title     string
	Committer gitstore.Identity
	// DryRun builds everything and pushes nothing.
	DryRun bool
	// Force writes over a remote that already holds a board.
	Force bool
}

// Report is what the migration did and what it could not carry over.
type Report struct {
	AlreadyMigrated                                            bool
	Verified                                                   bool
	Cards, Teams, Projects, Epics, Deadlines, Processes, Tasks int
	Events, Commits                                            int
	DoneFromSeeded, DoneWithoutJump                            int
	IssueCards                                                 []string
	UnattributedNotes                                          int
	Dangling                                                   []string
	Dropped                                                    []string
	IDMap                                                      map[string]string
}

// String renders the report for a person.
func (r Report) String() string {
	var b strings.Builder
	if r.AlreadyMigrated {
		b.WriteString("already migrated — nothing written\n")
		return b.String()
	}
	fmt.Fprintf(&b, "%d cards, %d tasks, %d teams, %d projects, %d epics, %d deadlines, %d processes\n",
		r.Cards, r.Tasks, r.Teams, r.Projects, r.Epics, r.Deadlines, r.Processes)
	fmt.Fprintf(&b, "%d events replayed into %d commits; end state verified: %v\n", r.Events, r.Commits, r.Verified)
	fmt.Fprintf(&b, "doneFrom seeded on %d done cards; %d done cards had no recorded jump (reopen falls back to the nudge)\n", r.DoneFromSeeded, r.DoneWithoutJump)
	fmt.Fprintf(&b, "%d unattributed notes (kept without an author)\n", r.UnattributedNotes)
	if len(r.IssueCards) > 0 {
		fmt.Fprintf(&b, "%d issue cards reduced to a link: %s\n", len(r.IssueCards), strings.Join(r.IssueCards, ", "))
	}
	for _, d := range r.Dangling {
		fmt.Fprintf(&b, "dangling reference cleared: %s\n", d)
	}
	for _, d := range r.Dropped {
		fmt.Fprintf(&b, "dropped: %s\n", d)
	}
	fmt.Fprintf(&b, "%d ids mapped (old → new)\n", len(r.IDMap))
	return b.String()
}

// migrationKey marks the reconcile commit so a second run recognises it.
const migrationKey = "Aeman-Migration"

// epoch dates the roster files, which have no creation time of their own in
// Projects v2 worth keeping.
var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Run migrates one board into remote, through storer (a scratch store: the
// clone is built there and pushed).
func Run(ctx context.Context, src Source, storer storage.Storer, remote gitstore.Remote, opts Options) (Report, error) {
	rep := Report{IDMap: map[string]string{}}
	marker := fmt.Sprintf("%s/%d", opts.Owner, opts.Board)

	// What is on the remote already?
	probe, err := gitstore.Clone(ctx, memory.NewStorage(), remote, gitstore.Options{}, 0)
	switch {
	case err == nil:
		head, err := probe.CommitObject(probe.Head())
		if err != nil {
			return rep, err
		}
		if strings.Contains(head.Message, migrationKey+": "+marker) && !opts.Force {
			rep.AlreadyMigrated = true
			return rep, nil
		}
		if !opts.Force {
			return rep, fmt.Errorf("remote %s already holds commits and no migration marker; pass --force to write over it", remote.URL)
		}
	case errors.Is(err, gitstore.ErrEmptyRepository):
	default:
		return rep, err
	}

	ex, err := src.LoadBoard(ctx, opts.Owner, opts.Board)
	if err != nil {
		return rep, fmt.Errorf("load board: %w", err)
	}
	m := newMapper(ex, &rep)
	snapshot := m.files(opts.Title)
	events := m.events()
	rep.Events = len(events)

	gopts := gitstore.Options{Committer: opts.Committer}
	if gopts.Committer.Name == "" {
		gopts.Committer = gitstore.Identity{Name: "aeman", Email: "aeman@localhost"}
	}
	repo, err := gitstore.Init(storer, gopts)
	if err != nil {
		return rep, err
	}

	// History: the rewound snapshot, then one commit per event.
	first := epoch
	if len(events) > 0 {
		first = events[0].at.Add(-time.Hour)
	}
	rewound := m.rewound()
	if _, err := repo.Commit(gitstore.Action{Name: "import", Summary: "import board " + marker, At: first}, writes(rewound)); err != nil {
		return rep, err
	}
	rep.Commits = 1
	for _, e := range events {
		m.apply(e.card, e.ev.Kind, e.ev.To)
		// One commit per event, whether or not the file changes: an event
		// whose payload is not a field (created, review-sent) is recorded
		// by its trailer alone.
		act := gitstore.Action{Name: e.ev.Kind, Actor: e.ev.Actor, At: e.at, Cards: []string{e.card.ItemID},
			Summary:    e.ev.Kind + ": " + e.card.Title,
			Changes:    []gitstore.Change{{Card: e.card.ItemID, Kind: e.ev.Kind, From: e.ev.From, To: e.ev.To}},
			AllowEmpty: true}
		if _, err := repo.Commit(act, []gitstore.FileWrite{{Path: m.pathOf(e.card), Data: m.render(e.card)}}); err != nil {
			return rep, err
		}
		rep.Commits++
	}
	// Truth: the exact snapshot, plus the migration record, marked.
	final := writes(snapshot)
	final = append(final, gitstore.FileWrite{Path: ".aeman/migration.yaml", Data: []byte(fmt.Sprintf("source: %s\ncards: %d\nevents: %d\n", marker, rep.Cards, rep.Events))})
	if _, err := repo.Commit(gitstore.Action{Name: "migrate", Summary: "migrate " + marker + ": reconcile to the snapshot", At: laterThan(events, first),
		Trailers: map[string]string{migrationKey: marker}}, final); err != nil {
		return rep, err
	}
	rep.Commits++

	// Verify byte for byte.
	for p, want := range snapshot {
		got, err := repo.ReadFile(p)
		if err != nil {
			return rep, fmt.Errorf("verify %s: %w", p, err)
		}
		if string(got) != string(want) {
			return rep, fmt.Errorf("verify %s: end state differs from the snapshot", p)
		}
	}
	rep.Verified = true
	if opts.DryRun {
		return rep, nil
	}
	if opts.Force {
		// Writing over a repository that already holds commits means the
		// remote's history goes too: an ordinary push would be refused as a
		// non-fast-forward update, and --force promised to write over it.
		return rep, repo.PushForce(ctx, remote)
	}
	return rep, repo.Push(ctx, remote)
}

func laterThan(events []event, first time.Time) time.Time {
	if len(events) == 0 {
		return first.Add(time.Hour)
	}
	return events[len(events)-1].at.Add(time.Second)
}

func writes(files map[string][]byte) []gitstore.FileWrite {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	out := make([]gitstore.FileWrite, 0, len(paths))
	for _, p := range paths {
		out = append(out, gitstore.FileWrite{Path: p, Data: files[p]})
	}
	return out
}

// ---- mapping ----------------------------------------------------------------------

type event struct {
	card *board.Card
	ev   board.Event
	at   time.Time
}

// mapper turns the loaded board into layout files with derived ids.
type mapper struct {
	b     board.Board
	items map[string]ghsource.Item // the GitHub side of each source card
	rep   *Report
	ids   map[string]string // old item id → new
	cards []*board.Card     // converted cards (rows + tasks)
	paths map[string]string // new id → path
	// the event log per card, in source order, kept apart from the files
	logs map[string][]board.Event
	// roster ids
	teamID    map[string]string
	projectID map[string]string
	processID map[string]string
}

func newMapper(ex ghsource.Export, rep *Report) *mapper {
	b := ex.Board
	m := &mapper{b: b, items: ex.Items, rep: rep, ids: rep.IDMap, paths: map[string]string{}, logs: map[string][]board.Event{},
		teamID: map[string]string{}, projectID: map[string]string{}, processID: map[string]string{}}
	// A board loaded without an explicit team order (a fake, an old snapshot)
	// still has its teams in the sprint pointers: no-team group first, then
	// by name, so the order is the same on every run.
	if len(b.TeamOrder) == 0 && len(b.SprintStates) > 0 {
		for team := range b.SprintStates {
			b.TeamOrder = append(b.TeamOrder, team)
		}
		sort.Strings(b.TeamOrder)
		m.b = b
	}
	// Roster ids first: cards refer to projects and processes by name, but
	// their files live under the roster's ids.
	for i, team := range b.TeamOrder {
		st := b.SprintStates[team]
		id := "_"
		if team != "" {
			id = gitstore.DeriveID(epoch, "team", team)
		}
		m.teamID[team] = id
		if st.ItemID != "" {
			m.ids[st.ItemID] = id
		}
		_ = i
	}
	for _, p := range b.Projects {
		id := gitstore.DeriveID(epoch, "project", p)
		m.projectID[p] = id
		if item := b.ProjectStates[p]; item != "" {
			m.ids[item] = id
		}
	}
	for _, col := range b.Epics {
		m.ids[col.ItemID] = gitstore.DeriveID(epoch, "epic", col.Project, col.Name)
	}
	for _, d := range b.Deadlines {
		m.ids[d.ItemID] = gitstore.DeriveID(epoch, "deadline", d.Project, d.Week)
	}
	for _, p := range b.Processes {
		id := gitstore.DeriveID(epoch, "process", p.Name)
		m.processID[p.Name] = id
		m.ids[p.ItemID] = id
	}
	for i := range b.Cards {
		m.ids[b.Cards[i].ItemID] = gitstore.DeriveID(createdAt(b.Cards[i]), "card", b.Cards[i].ItemID)
	}
	for i := range b.Tasks {
		m.ids[b.Tasks[i].ItemID] = gitstore.DeriveID(createdAt(b.Tasks[i]), "card", b.Tasks[i].ItemID)
	}
	m.convert()
	return m
}

func createdAt(c board.Card) time.Time {
	if t, err := time.Parse(time.RFC3339, c.CreatedAt); err == nil {
		return t
	}
	return epoch
}

// convert rewrites every card: new id, GitHub fields stripped, references
// remapped, notes given ids, doneFrom seeded, rank by board order.
func (m *mapper) convert() {
	all := make([]board.Card, 0, len(m.b.Cards)+len(m.b.Tasks))
	all = append(all, m.b.Cards...)
	all = append(all, m.b.Tasks...)
	ranks, _ := board.RankRebalance("", "", len(m.b.Cards))
	taskRanks, _ := board.RankRebalance("", "", len(m.b.Tasks))
	for i := range all {
		src := all[i]
		c := src
		isTask := i >= len(m.b.Cards)
		gh := m.items[src.ItemID]
		c.ItemID = m.ids[src.ItemID]
		c.GitHubID = src.ItemID
		if gh.URL != "" {
			c.Link = gh.URL
			m.rep.IssueCards = append(m.rep.IssueCards, src.ItemID)
		}
		c.Parent = m.ref(src.ItemID, "parent", src.Parent)
		c.ReviewOf = m.ref(src.ItemID, "reviewOf", src.ReviewOf)
		c.Task = m.ref(src.ItemID, "task", src.Task)
		if isTask {
			c.Rank = taskRanks[i-len(m.b.Cards)]
		} else {
			c.Rank = ranks[i]
		}
		c.Notes = nil
		for j, n := range src.Notes {
			if n.Author == "" {
				m.rep.UnattributedNotes++
			}
			when := createdAt(board.Card{CreatedAt: n.CreatedAt})
			c.Notes = append(c.Notes, board.Note{ID: gitstore.DeriveID(when, "note", src.ItemID, strconv.Itoa(j)), Body: n.Body, CreatedAt: n.CreatedAt, Author: n.Author, Source: "draft"})
		}
		m.logs[c.ItemID] = gh.Events
		if c.Progress >= 100 {
			if from, ok := lastJump(gh.Events); ok {
				c.DoneFrom = from
				m.rep.DoneFromSeeded++
			} else {
				m.rep.DoneWithoutJump++
			}
		}
		cp := c
		m.cards = append(m.cards, &cp)
		if isTask {
			m.paths[c.ItemID] = gitstore.TaskPath(m.processID[c.Process], c.ItemID)
			m.rep.Tasks++
		} else {
			p, _ := gitstore.CardPath(c.ItemID)
			m.paths[c.ItemID] = p
			m.rep.Cards++
		}
	}
	m.rep.Teams = len(m.b.TeamOrder)
	m.rep.Projects = len(m.b.Projects)
	m.rep.Epics = len(m.b.Epics)
	m.rep.Deadlines = len(m.b.Deadlines)
	m.rep.Processes = len(m.b.Processes)
	m.rep.Dropped = append(m.rep.Dropped, "Status field (Todo on every card; vestigial)")
}

// ref remaps an id-valued field; a target that is not on the board is
// cleared and reported.
func (m *mapper) ref(from, field, old string) string {
	if old == "" {
		return ""
	}
	if id, ok := m.ids[old]; ok {
		return id
	}
	m.rep.Dangling = append(m.rep.Dangling, fmt.Sprintf("%s.%s → %s", from, field, old))
	return ""
}

// lastJump is the from-side of the last progress event onto ≥100 — the walk
// Reopen does today.
func lastJump(events []board.Event) (int, bool) {
	from, ok := 0, false
	for _, e := range events {
		if e.Kind != board.EventProgress {
			continue
		}
		if to, err := strconv.Atoi(e.To); err == nil && to >= 100 {
			if f, err := strconv.Atoi(e.From); err == nil {
				from, ok = f, true
			}
		}
	}
	return from, ok
}

func (m *mapper) pathOf(c *board.Card) string { return m.paths[c.ItemID] }

func (m *mapper) render(c *board.Card) []byte {
	data, _ := gitstore.EncodeCard(gitstore.CardFile{Card: *c})
	return data
}

// files is the exact snapshot: every roster file and every card.
func (m *mapper) files(title string) map[string][]byte {
	out := map[string][]byte{}
	out[gitstore.BoardPath], _ = gitstore.EncodeBoard(gitstore.BoardFile{Schema: gitstore.SchemaVersion, Title: title})
	created := epoch.Format(time.RFC3339)
	teamRanks, _ := board.RankRebalance("", "", len(m.b.TeamOrder))
	for i, team := range m.b.TeamOrder {
		st := m.b.SprintStates[team]
		out[gitstore.TeamPath(m.teamID[team])], _ = gitstore.EncodeTeam(gitstore.TeamFile{Name: team, Rank: teamRanks[i], Created: created,
			Sprint: gitstore.SprintPointer{Current: st.Current, Previous: st.Previous}})
	}
	projRanks, _ := board.RankRebalance("", "", len(m.b.Projects))
	for i, p := range m.b.Projects {
		out[gitstore.ProjectPath(m.projectID[p])], _ = gitstore.EncodeProject(gitstore.ProjectFile{Name: p, Rank: projRanks[i], Created: created})
	}
	perProject := map[string]int{}
	epicRanks, _ := board.RankRebalance("", "", len(m.b.Epics))
	for i, col := range m.b.Epics {
		pid := m.projectID[col.Project]
		perProject[col.Project]++
		out[gitstore.EpicPath(pid, m.ids[col.ItemID])], _ = gitstore.EncodeEpic(gitstore.EpicFile{Name: col.Name, Rank: epicRanks[i], Created: created})
	}
	for _, d := range m.b.Deadlines {
		out[gitstore.DeadlinePath(m.projectID[d.Project], m.ids[d.ItemID])], _ = gitstore.EncodeDeadline(gitstore.DeadlineFile{Week: d.Week, Created: created})
	}
	procRanks, _ := board.RankRebalance("", "", len(m.b.Processes))
	for i, p := range m.b.Processes {
		out[gitstore.ProcessPath(m.processID[p.Name])], _ = gitstore.EncodeProcess(gitstore.ProcessFile{Name: p.Name, Project: p.Project, Paused: p.Paused, Rank: procRanks[i], Created: created})
	}
	for _, c := range m.cards {
		out[m.pathOf(c)] = m.render(c)
	}
	return out
}

// events is every card's log, oldest first, bound to the card it changes.
func (m *mapper) events() []event {
	var out []event
	for _, c := range m.cards {
		for _, ev := range m.logs[c.ItemID] {
			at, err := time.Parse(time.RFC3339, ev.At)
			if err != nil {
				continue
			}
			out = append(out, event{card: c, ev: ev, at: at})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

// rewound is the snapshot with every card wound back to before its first
// recorded event of each kind, so replaying the events walks forward to the
// snapshot as far as the log allows. The cards are mutated in place; the
// replay then applies each event's To.
func (m *mapper) rewound() map[string][]byte {
	for _, c := range m.cards {
		seen := map[string]bool{}
		for _, e := range m.logs[c.ItemID] {
			if !seen[e.Kind] {
				seen[e.Kind] = true
				m.apply(c, e.Kind, e.From)
			}
		}
	}
	files := m.files("")
	// The roster and board file are what they are from the start; only cards
	// were rewound. Re-render the board title properly.
	files[gitstore.BoardPath], _ = gitstore.EncodeBoard(gitstore.BoardFile{Schema: gitstore.SchemaVersion})
	return files
}

// apply sets the field an event kind names; kinds without a field (created,
// review-sent, …) change nothing in the file — their commit is the record.
func (m *mapper) apply(c *board.Card, kind, v string) {
	switch kind {
	case board.EventProgress:
		if n, err := strconv.Atoi(v); err == nil {
			c.Progress = n
		}
	case board.EventStage:
		if board.StageKey(v) == board.StageLocked || board.StageKey(v) == board.StageReview || board.StageKey(v) == board.StageRecurrent || board.StageKey(v) == board.StageDone || v == "" {
			c.Stage = board.StageKey(v)
		}
	case board.EventZone:
		c.Zone = board.ZoneKey(v)
	case board.EventSprint:
		c.SprintStart = v
	case board.EventWeek:
		c.Week = v
	case board.EventPlanBand:
		c.Plan = board.PlanBand(v)
	case board.EventTeam:
		c.Team = v
	case board.EventEpic:
		c.Epic = v
	case board.EventAssignee:
		if v == "" {
			c.Assignees = nil
		} else {
			c.Assignees = []string{v}
		}
	case board.EventDates:
		if start, end, ok := strings.Cut(v, ".."); ok {
			c.StartDate, c.Day = start, end
		}
	}
}
