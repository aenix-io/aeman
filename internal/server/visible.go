package server

import (
	"context"
	"fmt"
	"time"

	"github.com/aenix-io/aeman/pkg/board"
	"github.com/aenix-io/aeman/pkg/boardservice"
	"github.com/aenix-io/aeman/pkg/gitstore"
)

// visibleBackend is the store's backend as one visitor may use it (G17,
// G25): reads are projected onto the domains the request's rights can read —
// an unreadable primary is no board at all — and every write checks write
// access on the domain it targets, the destination of a move included. The
// rights ride the context (accessMiddleware, the MCP middleware); a context
// without rights is unrestricted, which only the server's own callers have.
type visibleBackend struct {
	boardservice.Backend
	primary string
	domains []string // every configured domain, primary first
}

// chosen is the caller's domain choice for a new team, project or process:
// the input's, else the context's, else the primary — and an unknown name
// is refused here, as a bad request, before any right is checked.
func (v *visibleBackend) chosen(ctx context.Context, explicit string) (string, error) {
	d := explicit
	if d == "" {
		d = board.DomainFrom(ctx)
	}
	if d == "" {
		return v.primary, nil
	}
	for _, name := range v.domains {
		if name == d {
			return d, nil
		}
	}
	return "", fmt.Errorf("%w: %q", gitstore.ErrUnknownDomain, d)
}

// full is the whole board, for deciding domains: the visible board cannot
// name the domain of what the visitor does not see, and a move's
// destination may be exactly that.
func (v *visibleBackend) full(ctx context.Context, bd board.Board) (board.Board, error) {
	if rightsFrom(ctx) == nil {
		return bd, nil
	}
	return v.Backend.LoadBoard(ctx, bd.Board)
}

// LoadBoard is the visitor's projection of the board.
func (v *visibleBackend) LoadBoard(ctx context.Context, boardID string) (board.Board, error) {
	bd, err := v.Backend.LoadBoard(ctx, boardID)
	if err != nil {
		return board.Board{}, err
	}
	r := rightsFrom(ctx)
	if r == nil {
		return bd, nil
	}
	if !r.canRead(v.primary) {
		return board.Board{}, boardservice.ErrForbidden
	}
	return board.Visible(bd, v.primary, r.readable), nil
}

// LoadCards omits what the visitor cannot read.
func (v *visibleBackend) LoadCards(ctx context.Context, bd board.Board, ids []string) ([]board.Card, error) {
	cards, err := v.Backend.LoadCards(ctx, bd, ids)
	if err != nil {
		return nil, err
	}
	r := rightsFrom(ctx)
	if r == nil {
		return cards, nil
	}
	out := make([]board.Card, 0, len(cards))
	for _, c := range cards {
		if r.canRead(c.Domain) {
			out = append(out, c)
		}
	}
	return out, nil
}

// domainOf is the domain a card is in — its own, or by the rule for a stub
// or a card the cache has not stamped.
func (v *visibleBackend) domainOf(ctx context.Context, bd board.Board, c board.Card) (string, error) {
	if c.Domain != "" {
		return c.Domain, nil
	}
	whole, err := v.full(ctx, bd)
	if err != nil {
		return "", err
	}
	for _, k := range whole.Cards {
		if k.ItemID == c.ItemID && k.Domain != "" {
			return k.Domain, nil
		}
	}
	if d, ok := whole.Domains[c.ItemID]; ok {
		return d, nil
	}
	if board.IsStateTitle(c.Title) {
		return v.stubHome(whole, c), nil
	}
	if d := board.DomainOf(c, board.Resolver(whole, v.primary)); d != "" {
		return d, nil
	}
	return v.primary, nil
}

// stubHome is where a roster stub lives: a column or deadline with its
// project, a task with its process, else the primary.
func (v *visibleBackend) stubHome(whole board.Board, c board.Card) string {
	switch c.Title {
	case board.EpicStateTitle, board.DeadlineStateTitle:
		if d, ok := board.Resolver(whole, v.primary).ProjectDomain(c.Project); ok {
			return d
		}
	case board.ProcessTaskTitle, board.ProcessStateTitle:
		for _, p := range whole.Processes {
			if p.Name == c.Process {
				if d, ok := whole.Domains[p.ItemID]; ok {
					return d
				}
			}
		}
	}
	return v.primary
}

// write checks the visitor may write the card's domain.
func (v *visibleBackend) write(ctx context.Context, bd board.Board, c board.Card) error {
	r := rightsFrom(ctx)
	if r == nil {
		return nil
	}
	d, err := v.domainOf(ctx, bd, c)
	if err != nil {
		return err
	}
	if !r.canWrite(d) {
		return boardservice.ErrForbidden
	}
	return nil
}

// writeMove checks both domains of a change that may re-file the card: where
// it is, and where the rule puts it after the change.
func (v *visibleBackend) writeMove(ctx context.Context, bd board.Board, c board.Card, change func(*board.Card)) error {
	r := rightsFrom(ctx)
	if r == nil {
		return nil
	}
	if err := v.write(ctx, bd, c); err != nil {
		return err
	}
	if board.IsStateTitle(c.Title) {
		return nil
	}
	whole, err := v.full(ctx, bd)
	if err != nil {
		return err
	}
	after := c
	change(&after)
	to := board.DomainOf(after, board.Resolver(whole, v.primary))
	if !r.canWrite(to) {
		return boardservice.ErrForbidden
	}
	return nil
}

func (v *visibleBackend) CreateCard(ctx context.Context, bd board.Board, in board.CreateInput) (board.Card, error) {
	if r := rightsFrom(ctx); r != nil {
		whole, err := v.full(ctx, bd)
		if err != nil {
			return board.Card{}, err
		}
		probe := cardFromInput(in, "")
		var target string
		switch {
		case in.Title == board.ProjectStateTitle, in.Title == board.SprintStateTitle,
			in.Title == board.ProcessStateTitle && in.Project == "":
			// The caller's choice, default the primary.
			if target, err = v.chosen(ctx, in.Domain); err != nil {
				return board.Card{}, err
			}
		case board.IsStateTitle(in.Title):
			target = v.stubHome(whole, probe)
		default:
			target = board.DomainOf(probe, board.Resolver(whole, v.primary))
		}
		if !r.canWrite(target) {
			return board.Card{}, boardservice.ErrForbidden
		}
		// The decided domain rides the input, so the optimistic copy the
		// cache hands out carries it from the first frame; the backend
		// applies the same rule and lands the file there.
		in.Domain = target
	}
	return v.Backend.CreateCard(ctx, bd, in)
}

func (v *visibleBackend) DeleteCard(ctx context.Context, bd board.Board, card board.Card) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.DeleteCard(ctx, bd, card)
}

func (v *visibleBackend) MoveCard(ctx context.Context, bd board.Board, card board.Card, afterID string) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.MoveCard(ctx, bd, card, afterID)
}

func (v *visibleBackend) AddNote(ctx context.Context, bd board.Board, card board.Card, text string) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.AddNote(ctx, bd, card, text)
}

func (v *visibleBackend) AppendEvent(ctx context.Context, bd board.Board, card board.Card, e board.Event) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.AppendEvent(ctx, bd, card, e)
}

func (v *visibleBackend) EditNote(ctx context.Context, bd board.Board, card board.Card, note board.Note, text string) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.EditNote(ctx, bd, card, note, text)
}

func (v *visibleBackend) DeleteNote(ctx context.Context, bd board.Board, card board.Card, note board.Note) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.DeleteNote(ctx, bd, card, note)
}

func (v *visibleBackend) SetDescription(ctx context.Context, bd board.Board, card board.Card, description string) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.SetDescription(ctx, bd, card, description)
}

func (v *visibleBackend) RenameCard(ctx context.Context, bd board.Board, card board.Card, title string) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.RenameCard(ctx, bd, card, title)
}

func (v *visibleBackend) SetStage(ctx context.Context, bd board.Board, card board.Card, stage board.StageKey) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.SetStage(ctx, bd, card, stage)
}

func (v *visibleBackend) SetProgress(ctx context.Context, bd board.Board, card board.Card, progress int) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.SetProgress(ctx, bd, card, progress)
}

func (v *visibleBackend) SetZone(ctx context.Context, bd board.Board, card board.Card, zone board.ZoneKey) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.SetZone(ctx, bd, card, zone)
}

func (v *visibleBackend) SetDay(ctx context.Context, bd board.Board, card board.Card, day string) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.SetDay(ctx, bd, card, day)
}

func (v *visibleBackend) SetStart(ctx context.Context, bd board.Board, card board.Card, date string) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.SetStart(ctx, bd, card, date)
}

func (v *visibleBackend) SetSprintStart(ctx context.Context, bd board.Board, card board.Card, date string) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.SetSprintStart(ctx, bd, card, date)
}

func (v *visibleBackend) SetPlan(ctx context.Context, bd board.Board, card board.Card, plan board.PlanBand) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.SetPlan(ctx, bd, card, plan)
}

func (v *visibleBackend) SetWeek(ctx context.Context, bd board.Board, card board.Card, week string) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.SetWeek(ctx, bd, card, week)
}

func (v *visibleBackend) SetTeam(ctx context.Context, bd board.Board, card board.Card, team string) error {
	if err := v.writeMove(ctx, bd, card, func(c *board.Card) { c.Team = team }); err != nil {
		return err
	}
	return v.Backend.SetTeam(ctx, bd, card, team)
}

func (v *visibleBackend) SetEpic(ctx context.Context, bd board.Board, card board.Card, epic string) error {
	if err := v.writeMove(ctx, bd, card, func(c *board.Card) { c.Epic = epic }); err != nil {
		return err
	}
	return v.Backend.SetEpic(ctx, bd, card, epic)
}

func (v *visibleBackend) SetProcess(ctx context.Context, bd board.Board, card board.Card, process string) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.SetProcess(ctx, bd, card, process)
}

func (v *visibleBackend) SetTask(ctx context.Context, bd board.Board, card board.Card, task string) error {
	if err := v.writeMove(ctx, bd, card, func(c *board.Card) { c.Task = task }); err != nil {
		return err
	}
	return v.Backend.SetTask(ctx, bd, card, task)
}

func (v *visibleBackend) SetPaused(ctx context.Context, bd board.Board, card board.Card, paused bool) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.SetPaused(ctx, bd, card, paused)
}

func (v *visibleBackend) SetAccumulate(ctx context.Context, bd board.Board, card board.Card, on bool) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.SetAccumulate(ctx, bd, card, on)
}

func (v *visibleBackend) SetProject(ctx context.Context, bd board.Board, card board.Card, project string) error {
	if err := v.writeMove(ctx, bd, card, func(c *board.Card) { c.Project = project }); err != nil {
		return err
	}
	return v.Backend.SetProject(ctx, bd, card, project)
}

func (v *visibleBackend) SetRecurrence(ctx context.Context, bd board.Board, card board.Card, cycle string) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.SetRecurrence(ctx, bd, card, cycle)
}

func (v *visibleBackend) SetAssignee(ctx context.Context, bd board.Board, card board.Card, login string) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.SetAssignee(ctx, bd, card, login)
}

func (v *visibleBackend) SetParent(ctx context.Context, bd board.Board, card board.Card, parent string) error {
	if err := v.writeMove(ctx, bd, card, func(c *board.Card) { c.Parent = parent }); err != nil {
		return err
	}
	return v.Backend.SetParent(ctx, bd, card, parent)
}

func (v *visibleBackend) SetReviewOf(ctx context.Context, bd board.Board, card board.Card, reviewOf string) error {
	if err := v.writeMove(ctx, bd, card, func(c *board.Card) { c.ReviewOf = reviewOf }); err != nil {
		return err
	}
	return v.Backend.SetReviewOf(ctx, bd, card, reviewOf)
}

func (v *visibleBackend) SetReviewRound(ctx context.Context, bd board.Board, card board.Card, round int) error {
	if err := v.write(ctx, bd, card); err != nil {
		return err
	}
	return v.Backend.SetReviewRound(ctx, bd, card, round)
}

// CardLog is the card's history when the backend keeps one; the visibility
// check happened when the service found the card on the visitor's board.
func (v *visibleBackend) CardLog(ctx context.Context, bd board.Board, id string) ([]board.Event, time.Time, error) {
	lr, ok := v.Backend.(boardservice.LogReader)
	if !ok {
		return nil, time.Time{}, nil
	}
	return lr.CardLog(ctx, bd, id)
}

// SetSprintState writes a team's pointer in the team's domain; a team not
// yet declared is declared in the primary.
func (v *visibleBackend) SetSprintState(ctx context.Context, bd board.Board, team, current, previous string) error {
	if r := rightsFrom(ctx); r != nil {
		whole, err := v.full(ctx, bd)
		if err != nil {
			return err
		}
		d, ok := board.Resolver(whole, v.primary).TeamDomain(team)
		if !ok {
			// A new team: declared where the caller chose, default the primary.
			if d, err = v.chosen(ctx, ""); err != nil {
				return err
			}
		}
		if !r.canWrite(d) {
			return boardservice.ErrForbidden
		}
	}
	return v.Backend.SetSprintState(ctx, bd, team, current, previous)
}
