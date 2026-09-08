---
name: size-cards
description: Size the unsized cards of a team or a person on the aeman board — S/M/L/XL by a fixed rubric, written back through the aeman MCP after the lead confirms. Use when a team lead asks to estimate, size or weigh cards, or before a planning meeting.
---

# Size cards

You size planning-board cards for a team lead: read each card, decide what it weighs by the rubric below, show the lead the table, and write the sizes back with `update_card`. The board sums sizes as points (S=1, M=2, L=4, XL=8) against each person's weekly capacity, so consistency matters more than precision: the same shape of card must always get the same size.

Sizing happens AFTER the fact, in a pass like this one — not card by card as work is planned. A card nobody has sized is not invisible meanwhile: the board weighs it as M, the middle of its own record. So the value of a pass is in the cards that are NOT M — the ones that would otherwise be under- or over-counted — and a card you would call M is a card the board already has right.

This is judgement work, card by card. Never derive a size from the length of the description, the team, or the kind of card by a rule; never write code that assigns sizes. Read the card and decide.

## Procedure

1. Find the cards. Use the aeman MCP `list_cards`:
   - a team: `list_cards view=team team=<key>` — read the keys from `get_board` (`metadata.teams`; `""` is the no-team group);
   - a person: `list_cards view=me user=<login>`;
   - a week on the Triage board: `list_cards view=triage team=<key>`, then keep the cards whose `status.triageWeek` is the Monday asked for;
   - the whole backlog of a team: `list_cards view=backlog team=<key>`.
   Keep the cards with no `spec.size`, unless the lead asked to re-size everything. Skip review cards (`spec.reviewOf` set) — a review is S by definition and the board already weighs an unsized one as S, so a pass over them changes nothing — and skip process turns (`spec.task` set) unless asked: a turn is planned work of its process and is normally S or M.
2. Read what you need. The listing is the board's row shape: title, team, zone, assignees, kind. When a title alone does not say what the work is, call `get_card` for the description — but do not open every card: a one-line card with no description is almost always S, and the rubric says so.
3. Size every card by the rubric. Write one line per card for the lead: `uid — title — size — why`, where *why* names the concrete deliverable in the card's own words (at most twelve words). Cards you are unsure about, mark with `?` and say what would decide it.
4. Show the table and STOP. Do not write anything until the lead has read it and said which sizes stand. They may change some; their word wins.
5. Write the sizes the lead confirmed: `update_card uid=<uid> size=<S|M|L|XL>`, one call per card. Report how many were written and the points total per person.

If the lead wants a person's capacity set too, `set_capacity login=<login> capacity=<points a week>` does it; 0 takes a number back, leaving the person with none — the board derives nothing. Working out what the number should be is the `derive-capacity` skill.

## The rubric

Estimate the work the card itself asks for. Do not guess at hidden scope; do not inflate a one-line card because the topic sounds important. Waiting on other people is not work.

- **S = 1 — up to a couple of hours.** One action in one place: send a message, ping a client, approve, rename, flip a setting, post something already prepared, a small config change, a review of a small PR, a short call. A card with no description and a short imperative title is almost always S. About half of all cards on this board are S.
- **M = 2 — half a day to a day.** One deliverable in one component: a small fix with a PR, a document of a page or two, a landing tweak with copy, a data pull, preparing a presentation from existing material, a meeting that needs preparation.
- **L = 4 — two to five days.** Several parts or several people: a feature touching more than one component, an integration, a design plus its implementation, a multi-step migration, a document that needs research, an event prepared from scratch, an investigation with an unknown cause.
- **XL = 8 — a week or more.** Epic-shaped: a new subsystem, a multi-week deliverable, something the description already breaks into phases, an umbrella that visibly holds more than L.

Rules of thumb:

- `[P1]` / `[Bug]` prefixes describe priority, not size.
- A card that lists several distinct deliverables is at least L.
- A recurring chore — "засинкать", "снять метрики", "пропинговать", "разослать" — is S unless its description says otherwise.
- A Project-board slot (a card with `spec.epic`) with only a title is L: a planned deliverable of a few days. XL only when it visibly holds more — subtasks, phases, an explicit estimate of a week or more.
- A card with subtasks does not need a careful size of its own: the board weighs it as the sum of its sized subtasks. Size the subtasks.
- Do not drift to M because M feels safe. S is the default for a one-line card.

## What the size is for

On the Triage board the person's column shows "points carried / points a week" and goes red past it; on the weekly meeting each week of the Triage board shows "points scheduled / points the teams can plan". Both numbers are only as good as the sizes under them: an unsized card is counted as M, so a board nobody has sized still reads, but every S and every L it holds is counted wrong by a point or two — which is exactly what a pass fixes.
