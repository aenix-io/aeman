package gitstore

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/aenix-io/aeman/pkg/board"
)

// The schema is a contract between server and repository (G18). A
// repository written by a newer server is refused — DecodeBoard returns
// ErrSchemaNewer — because this server would write states it does not
// understand. One written by an older server is brought to the current
// schema here, in one commit at start-up, so every replica agrees on the
// layout before it serves anything.

// upgrade is one version's rewrite of the tree: the files to write (or
// delete, with nil Data) beside the moved marker to bring a repository that
// stands at version-1 to it. Each is idempotent on its own — a tree that
// already has the shape writes nothing — and a step re-run finds nothing to
// do. A run that moves several versions at once hands each step the writes
// of the steps before it (earlier), so a later step reads a file as the
// earlier ones left it and its own write of the same path carries both
// rewrites rather than undoing the first.
type upgrade func(tree *object.Tree, earlier []FileWrite) ([]FileWrite, error)

// upgrades holds the rewrite each version asks for. Version 1 introduced
// the marker and rewrote nothing; version 2 is the hierarchy work's bump
// (docs/design/hierarchy.md, H8): the Process feature retired, card mirrors
// retired (the column rename it once carried went with the Project board); version 3 is the team
// backlog (docs/design/backlog.md, K8).
var upgrades = map[int]upgrade{
	2: func(tree *object.Tree, _ []FileWrite) ([]FileWrite, error) { return schema2(tree) },
	3: schema3,
}

// MigrateSchema brings board.yaml to SchemaVersion when it is older (or
// missing), in one "schema" commit that also carries every version's
// rewrite of the tree, and reports whether it did. A current repository is
// left alone; a newer one is refused.
func MigrateSchema(r *Repo) (bool, error) {
	var f BoardFile
	data, err := r.ReadFile(BoardPath)
	switch {
	case errors.Is(err, ErrNotFound):
		// A secondary domain seeded by hand: it gets a board file at the
		// current schema and nothing else.
	case err != nil:
		return false, err
	default:
		if f, err = DecodeBoard(data); err != nil {
			return false, err
		}
	}
	if f.Schema >= SchemaVersion {
		return false, nil
	}
	from := f.Schema
	f.Schema = SchemaVersion
	out, err := EncodeBoard(f)
	if err != nil {
		return false, err
	}
	writes := []FileWrite{{Path: BoardPath, Data: out}}
	// The rewrites ride the same commit as the marker: a replica that reads
	// the new marker reads the new shape, never one without the other.
	if head := r.Head(); !head.IsZero() {
		c, err := object.GetCommit(r.s, head)
		if err != nil {
			return false, err
		}
		tree, err := c.Tree()
		if err != nil {
			return false, err
		}
		for v := from + 1; v <= SchemaVersion; v++ {
			step, ok := upgrades[v]
			if !ok {
				continue
			}
			more, err := step(tree, writes[1:])
			if err != nil {
				return false, fmt.Errorf("gitstore: schema %d: %w", v, err)
			}
			writes = lastWriteWins(append(writes, more...))
		}
	}
	_, err = r.Commit(Action{Name: "schema", Summary: fmt.Sprintf("schema: %d → %d", from, SchemaVersion), At: time.Now()},
		writes)
	if err != nil {
		return false, err
	}
	return true, nil
}

// Schema 2 is one bump with several steps, specified in order in
// docs/design/hierarchy.md ("Migration"):
//
//  1. the processes/ tree goes (the Process feature retired);
//  2. `process`, `task` and `accumulate` come off every card — a filed turn
//     is ORPHANED, a card of its week and its zone and nothing more;
//  3. the column stubs moved from projects/<id>/epics/ to
//     projects/<id>/scopes/ and the card key `epic` was written as `scope` —
//     retired with the Project board (H15): the projects/ tree and every card
//     in a column are left exactly as they are, under either spelling;
//  4. `mirrors` comes off every card (card mirrors retired);
//  5. a card grouped UNDER a card in a column is ungrouped: the board reads
//     no project card, so the link names nothing (H4, H15).
//
// The steps that rewrite a card COMPOSE: one pass decides per file whether
// any of them applies and writes the file once with all of them applied —
// independent passes would each produce a FileWrite for the same path and
// the last would win, undoing the others. A card none of the steps touch is
// not rewritten, so the commit touches exactly what the retired features
// touched. Step 5 needs to know which cards stand in a column before it
// reads the rest, so the tree is walked twice: once for the column cards'
// ids, once for the writes.

// retiredCardKeys are the front-matter keys schema 2 takes off a card: the
// Process feature's (steps 2) and the mirrors list (step 4). The codec no
// longer knows them, so on read they sit in Extra, which is where this pass
// finds them.
var retiredCardKeys = map[string]bool{"process": true, "task": true, "accumulate": true, "mirrors": true}

// retiredProcessesDir is the directory the Process feature kept its
// processes and tasks in; the whole tree goes (step 1).
const retiredProcessesDir = "processes/"

// cardsInColumns lists the ids of the cards that stand in a column, read off
// their front-matter keys alone — what step 5 needs to recognise a card
// grouped under one of them, and what every step needs to leave one alone.
func cardsInColumns(tree *object.Tree) (map[string]bool, error) {
	in := map[string]bool{}
	err := tree.Files().ForEach(func(f *object.File) error {
		kind, ids := ParsePath(f.Name)
		if kind != PathCard {
			return nil
		}
		data, err := readAll(f)
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
		cf, err := DecodeCard(ids[0], data)
		if err != nil {
			return nil //nolint:nilerr // a broken card is left as every reader leaves it
		}
		if cf.ProjectCard {
			in[ids[0]] = true
		}
		return nil
	})
	return in, err
}

// schema2 walks the tree and emits every write the bump asks for.
func schema2(tree *object.Tree) ([]FileWrite, error) {
	inColumn, err := cardsInColumns(tree)
	if err != nil {
		return nil, err
	}
	var writes []FileWrite
	err = tree.Files().ForEach(func(f *object.File) error {
		if strings.HasPrefix(f.Name, retiredProcessesDir) {
			writes = append(writes, FileWrite{Path: f.Name})
			return nil
		}
		kind, ids := ParsePath(f.Name)
		if kind != PathCard || inColumn[ids[0]] {
			// A project card is the retired Project board's, and stays
			// byte for byte what it was (H15).
			return nil
		}
		data, err := readAll(f)
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
		keys, err := frontKeys(data)
		if err != nil {
			// A card the loader already reports as broken is not this
			// step's to mend; it is left as it is, as every reader leaves it.
			return nil //nolint:nilerr // see above
		}
		// Which steps this file MIGHT need, read off its keys: the retired
		// keys (steps 2 and 4), and a parent — which is step 5 when the
		// parent stands in a column, known only once the card is read.
		touched, hasParent := false, false
		for _, k := range keys {
			switch {
			case retiredCardKeys[k]:
				touched = true
			case k == "parent":
				hasParent = true
			}
		}
		if !touched && !hasParent {
			return nil
		}
		cf, err := DecodeCard(ids[0], data)
		if err != nil {
			return nil //nolint:nilerr // see above
		}
		if inColumn[cf.Card.Parent] {
			cf.Card.Parent = ""
			touched = true
		}
		if !touched {
			return nil // a parent that stands in no column: nothing to do here
		}
		kept := cf.Extra[:0:0]
		for _, x := range cf.Extra {
			if !retiredCardKeys[x.Key] {
				kept = append(kept, x)
			}
		}
		cf.Extra = kept
		out, err := EncodeCard(cf)
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
		writes = append(writes, FileWrite{Path: f.Name, Data: out})
		return nil
	})
	return writes, err
}

// lastWriteWins keeps one write per path, the last, in first-seen order: a
// later step's write of a file already carries the earlier step's rewrite
// (it read the file through the earlier writes), so two writes of one path
// in one commit would only make the earlier one dead weight.
func lastWriteWins(writes []FileWrite) []FileWrite {
	at := map[string]int{}
	out := make([]FileWrite, 0, len(writes))
	for _, w := range writes {
		if i, ok := at[w.Path]; ok {
			out[i] = w
			continue
		}
		at[w.Path] = len(out)
		out = append(out, w)
	}
	return out
}

// Schema 3 is the team backlog, specified in docs/design/backlog.md
// ("Migration: schema 2 → 3", K8). One pass over the cards, each rewritten
// at most once with every step applied:
//
//  1. a task with a week and no date and no sprint: when the week is still
//     AHEAD it is PARKED — accepted and not yet in hand, which is exactly the
//     backlog; when the week has COME it stood on the day boards as a debt, so
//     it stays in work with that week's Monday to Friday as its dates; when
//     it is finished it only loses the week. Either way the week goes;
//  2. a task with a week and a date or a sprint keeps them and loses the
//     week — it is in work;
//  3. an open task that would have stood in the Triage strip — no date, no
//     sprint, no week, not parked — is PARKED;
//  4. deadline stubs and project cards are the retired Project board's and
//     are left as they are (H15); planning cards have no week already
//     (schema 2);
//  5. an open task on review with no review card gets the parked review
//     card nobody has taken (K10, schema3Reviews).
//
// A subtask or a review card with a stray week loses it and is not parked:
// it rides the card it belongs to. The rank is untouched. Telling a subtask
// from a task needs the parent's type, so the cards are read first and
// judged second.
func schema3(tree *object.Tree, earlier []FileWrite) ([]FileWrite, error) {
	over := make(map[string][]byte, len(earlier))
	gone := map[string]bool{}
	for _, w := range earlier {
		if w.Data == nil {
			gone[w.Path] = true
			continue
		}
		over[w.Path] = w.Data
	}
	type held struct {
		path string
		file CardFile
	}
	var cards []held
	byID := map[string]board.Card{}
	sprints := map[string]string{} // team name → its current sprint
	err := tree.Files().ForEach(func(f *object.File) error {
		if gone[f.Name] {
			return nil
		}
		kind, ids := ParsePath(f.Name)
		if kind == PathTeam {
			data, err := readAll(f)
			if err != nil {
				return fmt.Errorf("%s: %w", f.Name, err)
			}
			if tf, err := DecodeTeam(data); err == nil && tf.Sprint.Current != "" {
				sprints[tf.Name] = tf.Sprint.Current
			}
			return nil
		}
		if kind != PathCard {
			return nil
		}
		orig, err := readAll(f)
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
		data, rewritten := over[f.Name]
		if !rewritten {
			data = orig
		}
		cf, err := DecodeCard(ids[0], data)
		if err != nil {
			return nil //nolint:nilerr // a broken card is left as every reader leaves it
		}
		if cf.ProjectCard {
			// A project card is no card of the board any more (H15): left
			// as it is, and no parent of anything this step judges.
			return nil
		}
		if rewritten && cf.Week == "" {
			// An earlier step's write went through the codec, which no longer
			// writes a week (K7): the week this step decides on is the one the
			// file had in the tree.
			if was, err := DecodeCard(ids[0], orig); err == nil {
				cf.Week = was.Week
			}
		}
		cards = append(cards, held{path: f.Name, file: cf})
		byID[cf.Card.ItemID] = cf.Card
		return nil
	})
	if err != nil {
		return nil, err
	}
	today := board.TodayIso()
	var writes []FileWrite
	for _, h := range cards {
		c := h.file.Card
		p, hasParent := byID[c.Parent]
		// board.IsSubtask's reading: a parent that is not a planning card —
		// or not in this tree at all (deleted, another repository) — makes
		// it a subtask.
		subtask := c.Parent != "" && (!hasParent || !board.IsPlanning(p))
		if !parkForSchema3(&c, h.file.Week, subtask, today, sprints[c.Team]) {
			continue
		}
		h.file.Card = c
		out, err := EncodeCard(h.file)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", h.path, err)
		}
		writes = append(writes, FileWrite{Path: h.path, Data: out})
	}
	all := make([]board.Card, 0, len(cards))
	for _, h := range cards {
		all = append(all, h.file.Card)
	}
	more, err := schema3Reviews(all, time.Now())
	if err != nil {
		return nil, err
	}
	return append(writes, more...), nil
}

// schema3Reviews is schema 3's last step (K10, docs/design/backlog.md
// "Reviews"): every OPEN task already on review with no review card gets the
// card putting a task on review makes now — «review: <title>», of the task's
// team, with nobody on it, parked in that team's backlog, carrying the
// task's description — under a fresh ULID created at the bump and ranked at
// the TOP of the board's order, before the first card, so it shows first in
// its backlog (K10); several stand in a fixed order, the oldest task's
// first. A task with a review card of any kind gets none (a finished one is
// the re-review's to reactivate), so the step re-run finds nothing to do.
func schema3Reviews(cards []board.Card, now time.Time) ([]FileWrite, error) {
	reviewed := map[string]bool{}
	first := ""
	for _, c := range cards {
		if c.ReviewOf != "" {
			reviewed[c.ReviewOf] = true
		}
		if c.Rank != "" && (first == "" || c.Rank < first) {
			first = c.Rank
		}
	}
	var asking []board.Card
	for _, c := range cards {
		if c.Stage != board.StageReview || reviewed[c.ItemID] || board.IsPlanning(c) ||
			c.ReviewOf != "" || board.Complete(c.Stage, c.Progress) {
			continue
		}
		asking = append(asking, c)
	}
	sort.SliceStable(asking, func(i, j int) bool {
		if asking[i].CreatedAt != asking[j].CreatedAt {
			return asking[i].CreatedAt < asking[j].CreatedAt
		}
		return asking[i].ItemID < asking[j].ItemID
	})
	// Ranked from the last up, each before the one after it, so the oldest
	// task's review ends first of all.
	ranks := make([]string, len(asking))
	next := first
	for i := len(asking) - 1; i >= 0; i-- {
		rank, err := board.RankBetween("", next)
		if err != nil {
			return nil, err
		}
		ranks[i], next = rank, rank
	}
	var writes []FileWrite
	for i, c := range asking {
		rank := ranks[i]
		// A pure function of the tree, so two replicas bumping it before
		// either pushes write the same file at the same path: the id is
		// derived from the task's (its ULID time, the "schema3-review"
		// namespace, the task's id), and the created time is that ULID
		// time too. A task id that is not a ULID falls back to the bump's
		// own moment for the time.
		at := now
		if t, err := IDTime(c.ItemID); err == nil {
			at = t
		}
		id := DeriveID(at, "schema3-review", c.ItemID)
		created := at.UTC().Format(time.RFC3339)
		review := CardFile{Card: board.Card{
			ItemID: id, Title: "review: " + c.Title, Team: c.Team, Zone: board.ZoneGray,
			ReviewOf: c.ItemID, Parked: true, Rank: rank, CreatedAt: created,
			Description: board.StripEventLines(c.Description),
		}}
		out, err := EncodeCard(review)
		if err != nil {
			return nil, err
		}
		p, err := CardPath(id)
		if err != nil {
			return nil, err
		}
		writes = append(writes, FileWrite{Path: p, Data: out})
	}
	return writes, nil
}

// PlaceForSchema3 is schema 3's rule for one card as it stood before the
// bump — its week included — and reports whether it changed anything: what
// the bump writes, and what an importer writing the current schema applies
// to a card it reads from a board with weeks (K8). subtask says whether the
// card rides a TASK parent; today is the day the week is judged against;
// teamSprint is the current sprint of the card's team ("" for none), which a
// task whose week has come joins.
func PlaceForSchema3(c *board.Card, week string, subtask bool, today, teamSprint string) bool {
	return parkForSchema3(c, week, subtask, today, teamSprint)
}

// parkForSchema3 is schema 3's rule for one card, and reports whether it
// changed anything.
func parkForSchema3(c *board.Card, week string, subtask bool, today, teamSprint string) bool {
	changed := false
	if week != "" {
		// Steps 1 and 2: only a task that answers for its own place is
		// placed; anything else just loses the week.
		task := !board.IsStateTitle(c.Title) && !board.IsPlanning(*c) &&
			c.ReviewOf == "" && !subtask
		if task && !board.Complete(c.Stage, c.Progress) {
			switch {
			case week > board.MondayOf(today) && !board.InWork(*c):
				// A week ahead: the backlog.
				c.Parked = true
			case week <= board.MondayOf(today):
				// A week that has come: a debt on the day boards, kept
				// there. Its week's Monday to Friday become its dates when it
				// has none, and with no sprint it joins its team's current
				// one — the week is what kept it on its owner's Me board
				// (D17), and an open card of a past day with no sprint is on
				// no Me board.
				if c.StartDate == "" {
					c.StartDate, c.Day = week, board.AddDays(week, 4)
				}
				if c.SprintStart == "" && teamSprint != "" {
					c.SprintStart = teamSprint
				}
			}
		}
		changed = true
	}
	// Step 3: the strip.
	if board.Unplaced(*c, subtask) {
		c.Parked = true
		changed = true
	}
	return changed
}
