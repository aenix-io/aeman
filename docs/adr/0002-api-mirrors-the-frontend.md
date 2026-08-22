# ADR 0002 — The API and MCP mirror the frontend, not the backend

**Status:** accepted · 2026-08-22

## Decision

The HTTP API and the MCP tool set expose the entities a person sees in
the frontend — boards, cards, teams, projects, epic columns, deadlines,
processes, tasks — under those names, with those relationships and
those rules. They do not expose the backend's model. A GitHub Projects
v2 item with a `Process` text field and a title of `aeman:process-state`
is an implementation detail; a *process* is the entity.

What the frontend lets a person do, the API and MCP let an agent do, by
the same name: the Project tab's column is `add_epic`, its deadline line
is `add_deadline`, its "Mark as done" is the card's stage. An entity on
screen with no endpoint is a gap, and an endpoint with no entity on
screen is a smell.

## Why

Two kinds of client work the board: people through the frontend, and
agents through MCP. They must see the same world, or they cannot work
together on it — an agent that files a card by "the Epic text field"
and a person who drags a slot on "the Project board" are not talking
about the same thing, and the first time the two disagree (a card in a
column but out of every weekly plan, because only the frontend knew to
set the band) the board stops being shared.

Our model is the product. The backend is whatever store is convenient
today; it has already changed shape several times under the same API
and could be replaced. Projecting its model outward would freeze the
accident of today's storage into everybody's integration.

## Consequences

- Every rule lives in the service (`pkg/boardservice`), where every door
  passes — not in the frontend. When the frontend does something the
  service does not (adding the band on team assignment), that is a bug
  in the service, not a feature of the frontend.
- Naming follows the frontend. When the Plan tab became Project, the
  view, the parameter and the tools were renamed with it, and the GitHub
  board moved to `board` to free the word.
- A new tab ships with its endpoints and tools, documented in
  `docs/api.md`, or it does not ship.
- MCP tool descriptions explain the model to an agent the way the UI
  explains it to a person: what a thing is, what it belongs to, where a
  value is counted from — so an agent can act without reading the code.
