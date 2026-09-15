# aeman API and MCP server

aeman exposes its board service three ways: the embedded UI, a JSON HTTP API under `/api/v1`, and an MCP (Model Context Protocol) server for AI agents. All three call the same board logic over a shared in-memory store, so they behave identically — and every change any of them makes is committed to the board's git repository and pushed to all connected clients over the WebSocket watch stream.

The API is Kubernetes-style: a small set of **resources** (`Board`, `Card`, `Sprint`, `Note`, `Ordering`) shaped as `{kind, metadata, spec, status}`, LIST with selectors that reproduce the UI's views, PATCH for edits, and **actions** for everything with board-level rules. Clients state intent; the server applies the rules (clamps, review links, the date model of [dates.md](dates.md)) and streams the results. The design rationale lives in [design/api-redesign.md](design/api-redesign.md); the storage in [design/git-backend.md](design/git-backend.md).

## Authentication and access

The server holds **a credential of its own** — `AEMAN_GIT_TOKEN`, or `AEMAN_GIT_TOKEN_<NAME>` per repository — for fetching and pushing the board's repositories, for the membership checks behind the assignee pickers, and for resolving issue/PR titles named in card descriptions. A board spanning two organisations gives each repository its own token: one narrow enough for either cannot reach both. Visitors never push; the server commits on their behalf, authored with their login.

The board lives on one **forge** — GitHub or GitLab (gitlab.com or self-hosted) — picked by `--forge`/`AEMAN_FORGE` or, unset, by the primary repository's host (see [Configuration](#configuration)). A visitor is identified by their forge login: from the session in the self-hosted OAuth mode (one client id/secret pair — `AEMAN_GITHUB_CLIENT_ID`/`_SECRET` or `AEMAN_GITLAB_CLIENT_ID`/`_SECRET`), or, in the default local mode, from whoever the credential the server resolved belongs to — the forge's token variables, the OS keychain (`aeman login`), then the forge's CLI signed in on the machine (`gh`, `glab`); see [aeman login](#aeman-login). Access follows the visitor's **own rights on each repository** (see Domains below), asked of the forge with the visitor's token. GitHub: the repository's `permissions` block — `pull` reads, `push`/`maintain`/`admin` write. GitLab: the project's access level — Reporter (20) reads, Developer (30) and above write; a Guest sees the project but not its board; a public or internal project reads for anyone signed in. Read access to a repository shows its part of the board, write access allows changes to it. The decision is made per request from the forge and cached briefly. A visitor who cannot read the primary repository has no board at all (403); a mutation on a card whose repository the visitor cannot write is refused (403).

Board members — the people the assignee and reviewer pickers offer — are those who can read the repository, resolved with the server's token: GitHub asks each login's collaborator permission; GitLab reads the project's member list, inherited group members included, which also supplies display names and avatars. On GitHub an avatar is built from the login (the avatars CDN) and there are no display names; on GitLab both come from the forge's user directory, so `metadata.members[].name` is filled on GitLab boards.

The local MCP server (`aeman mcp`) runs on stdio, or as a loopback HTTP daemon shared by every client on the machine (`--listen`); either way it is one process on its own clone. It pushes with `AEMAN_GIT_TOKEN` when that is set; otherwise the credential comes from `GITHUB_TOKEN`/`GH_TOKEN` (GitHub) or `GITLAB_TOKEN` (GitLab), then the OS keychain written by [`aeman login`](#aeman-login), then the forge CLI's own token (`gh auth token` / `glab config get token --host <host>`). The actor is whoever that credential belongs to, asked of the forge for a token from the environment or the keychain and read from the tool itself for a CLI one (`gh api user` / `glab api user`): a stored bot token is attributed to the bot, not to whoever the machine's `gh` is signed in as. A push-only credential belongs in `AEMAN_GIT_TOKEN` for that reason. When a token exists but `/user` cannot name its owner — during a transient forge outage or for a GitHub App installation token, for example — read-only MCP tools and `carry_over` with `dryRun=true` remain available, while mutations are refused until the owner can be resolved. This keeps the credential available without recording the user's work as a server-authored commit. `AEMAN_GIT_TOKEN` is the exception, and deliberately so — it names the server's own push credential and never named a person, so with it set the push and the actor can be two different accounts. In the self-hosted mode the same tool set is mounted over HTTP at `/mcp`, authenticated with per-user OAuth tokens, and the rights above apply to every tool call.

## The board

A server serves **one board**: the repositories it was started with (`--repo name=url`, repeatable, the primary first). There is no board addressing on the API — `owner`/`board` query parameters from earlier versions are ignored — and MCP tools take no board argument. The board's name is its primary repository's name.

**"Project" means aeman's own planning entity** — a group of epic columns on the Project board — never a repository or a GitHub board. `project` is a card filter (`/views/project/cards?project=cozystack`) and the subject of its own endpoints.

### Domains

Each repository of the board is a **domain** — a visibility boundary. A card's domain is never chosen per card; it follows one rule, linked cards first: a review card lives with the card it reviews, a subtask with its parent, a process iteration with its task; otherwise a card under a project lives where the project is declared, else where its team is declared; the no-team group and anything unresolved live in the primary. A change that moves a card across that rule (re-filing it under another project, say) moves the file between repositories — and its review card and subtasks with it.

Teams, projects and processes are declared in the domain the caller picks: the optional `domain` field on `POST /projects`, `POST /processes` and `PATCH /sprints` (a team is declared by its first sprint write), default the primary. A process with a project lives with the project.

**A name is declared once across the board's repositories, per kind of entry.** A team, project or process name is what cards refer to (`team: portal`) and what the domain rule resolves, so the same name cannot be declared twice for the same kind — in one repository or in two. The three kinds keep separate namespaces: a team and a project may share a name, and nothing refuses that. A create or a rename into a name any domain already carries is refused (422), whether or not the caller can read that domain; the UI checks the same against the roster it sees before sending. Repositories that already collide when the server starts — two boards connected as one — are refused at start-up, with both files named, so the fix is one rename away. A collision that reaches the files of a running server (a direct git write, two replicas racing) resolves on read to the oldest declaration; the other is an alias whose cards still count, listed by `GET /api/healthz` for a maintainer to rename by hand.

`GET /board` lists the visitor's readable domains as `metadata.domains` — primary first, each with a `writable` flag and the logins that can read it — and every card carries `status.domain`. Always, one repository or many: the store stamps every entry with its repository's NAME, the primary's included, so the list is what a client compares those stamps against. A board of ONE repository lists exactly one domain and no card is "somewhere else"; that is how a client tells the two cases apart, by the COUNT, not by the list being absent. A domain the visitor cannot read is simply absent — no cards, teams or projects from it, no watch frames about it — not empty.

### The planning entities

A project carries **deadlines**: a line across the grid on a given week. One project holds at most one per week, so dragging one of its lines onto another merges them — but two projects can each have something due the same week, and those are two lines.

A slot's **row is the week of its `dates.start`** — always derived, never stored beside the dates where the two could drift. `spec.week` is the week a card with no dates is scheduled for, which is its column on the Triage board. It is a **Monday**, and any other day is refused (422, `a week on the Triage board is a Monday`) — unlike a deadline's date, which resolves to its Monday: a column IS a Monday, so a week that is not one would stand in no column at all. Setting a week on a card filed under an epic is refused too (422) — its week follows its start date — though re-asserting the week the slot already derives is a harmless no-op, which is what a client echoing the visible week back does. A **process turn** keeps its own occurrence: the weeks from the one its task came due in through the week before the next due date (`board.CycleWindow`), since a turn carried past that stands where the NEXT turn belongs and the two read as one process running twice. Outside it is **422** — except for a task that ACCUMULATES, whose turns are meant to pile up and may go anywhere, and except for a turn whose occurrence is already PAST, which may come forward into the CURRENT week: its days ran out, it is on no day board, and that is the only gesture that brings it back. A turn of a task with no calendar at all (a per-sprint one) has no occurrence and does not move in time (422) — once it stands in a week, that is: a turn with no week yet is in no occurrence and nothing bounds it.

A project also holds **processes** — recurring work the team keeps doing and wants to see itself doing. A process groups **tasks**; each task says what every iteration is called and says, its cycle (counted on the calendar from its start date, not from when the last iteration closed), the team the iterations land in, and the standing owner. The server files what each week is owed by itself, as the week arrives — nothing to call. An open iteration holds the next one back — the stuck card *is* the process, and it goes overdue where it is — unless the task `accumulate`s, for work where unpaid months must pile up as separate cards. Every iteration is a fresh copy of the task: a renamed live card stays renamed. See [`docs/design/processes.md`](design/processes.md).

A **column** of the Project board is the pair `(project, epic)`. Epic names are unique only *within* a project, so every project can have its own `Docs` or `Auth`, and a card names both halves. Anything acting on a column — filing a card, deleting, renaming, reordering — names both.

## HTTP API

Base path: `/api/v1`. Requests and responses are JSON. A write that leaves nothing to show answers `204 No Content` with no body; one that creates or changes a resource answers with that resource.

Errors are [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem details, `Content-Type: application/problem+json`:

```json
{
  "type": "about:blank",
  "title": "Unprocessable Entity",
  "status": 422,
  "detail": "team already exists: \"portal\"",
  "code": "teamExists"
}
```

`detail` is the sentence to show a person. `code` is what a program branches on: it is stable, and for a rule the board refuses with it is that rule's own name — `cardNotFound`, `teamExists`, `notAMonday`, `subtaskWeek`. `type` is always `about:blank` (there is no page per problem; `code` carries the identity) and `title` is the status's own text. `actionUrl`, when present, is a page that fixes the refusal — installing the board's GitHub App — for a client to offer as a button.

The status says whose fault it was: 400 bad request — a body that does not parse (`invalidBody`), an unknown zone, size, stage or intent (`unknownZone`, `unknownSize`, `unknownStage`, `unknownIntent`), a selector or day that is not one (`invalidSelector`, `invalidDay`, `invalidAsOf`), a missing number (`pointsRequired`, `capacityRequired`), too many cards in one request (`tooManyCards`), an unknown `domain` (`unknownDomain`); 401 not authenticated (`notAuthenticated`, `authorizationExpired`); 403 no read access to the board or no write access to the card's domain (`forbidden`, `accessUndecided`), a card refused by someone it is not on or removed by its assignee when someone else put it there (`notYoursToRefuse`, `notYoursToRemove`), or a write from another site (`crossSiteBlocked`); 404 a card, note or board nobody has (`cardNotFound`, `noteNotFound`, `noSuchView`), a gesture made from a board that does not draw it (`gestureNotOffered`, `cardNotOnBoard`), or a path no route serves (`unknownRoute`) — which is also the answer when a door does not have the method that was asked of it: the SPA's catch-all carries no method and so takes every one, which is why the mux's own 405 never applied and a wrong verb used to reach the page. a wrong verb on the index is refused at another address: only its GET is wired, and a GET pattern serves HEAD too, so every other verb is redirected (307) to `/api/v1/` and refused by the catch-all there — `/api/v1/` is the catch-all's, never the index's; 409 a write to a day that is over for the card's team (`dayIsARecord`); 410 and 501 a past day history no longer reaches or never kept (`historyTruncated`, `noHistory`); 413 the request body is larger than the server accepts (`bodyTooLarge`); 422 a rule refused the change — a title past its length cap or a date that is not a real board day included; 502 the forge could not be reached (`upstreamFailed`); 503 the board's app is not installed yet (`setupRequired`, with `actionUrl`). The sign-in endpoints are outside this rule: `/oauth/token` and `/oauth/register` answer in RFC 6749's shape (`error` as a code, `error_description` as the sentence), and the browser-facing pages under `/auth/` and the rest of `/oauth/` still answer `{"error": "<sentence>"}`. The WebSocket handshake on `/api/v1/views/{view}/watch` is outside it too: its own refusals (a bad `Origin`, a malformed handshake) are the library's `text/plain` errors, not problems.

`GET /api/v1` answers the board's name and version, the MCP mount point, and where the description of this surface lives.

### The board you are standing on

A person opens a BOARD and presses something on it, and which board it was decides what the press meant. That board is a **path segment**: `/api/v1/views/{view}/cards` lists what it draws, creates the kind of card it makes, and answers the gestures it offers. The views are `me`, `team`, `triage`, `backlog`, `project`, and `all` — the escape hatch for a caller with no board at all, which has to be said out loud and is a default nowhere. `GET /api/v1/views` is the catalog: each board with the gestures it draws.

It is **not** a namespace, though it is shaped like one: the same card is drawn on several boards at once, and both listings are telling the truth. The segment says where the CALLER is standing. The card keeps its own address — `/api/v1/cards/{uid}`, its subresources, and the actions that mean the same thing wherever they are made (defer, in-progress, reopen, move, send-to-review, mirror). Design: [design/view-scoped-api.md](design/view-scoped-api.md).

Three things follow from the segment:

- **A create means what that board means by it.** Typed into `me` the card is yours, today, in the sprint, and in the **unplanned** band — that is the only one the Me board adds in, because something that came up today is unplanned by definition and the other three bands are the plan, which is the lead's to make on the team's grid (any other zone there is a 422, and a SUBTASK is exempt — it takes its parent's band, not the board's); into `team` it lands in the Unassigned column unless somebody is named; into `triage` it is scheduled for a `week`, and stands on no day unless that week is the one being WORKED — a card started in the current week belongs to today as well, which is what the board's own add form in that row does, while one filed into a week ahead waits for its Monday (B1) and dates on it are a 422; into `backlog` it is parked on the team's shelf; into `project` it is a slot under a column. Each board refuses the fields it does not own — a parked card typed into a day, a column named on the Me board — with a **422** that says which field it was, and a board asked for the one field its whole gesture is (a Triage card with no week) answers 422 too.
- **A gesture is answered by the board that draws it.** `remove` on all of them, `place` and `untriage` on `triage`, `finished-earlier` on the day boards — and all four on `all`, which is no board and is refused by none. Asked of another board it is **404**, the same answer as a route that is not there.
- **A gesture on a card that board does not draw is 404.** A person cannot press × on a card they cannot see; a caller that named a board is held to it. The check is the listing itself, with the same defaults (the day is today, the team the card's own, the person the caller), and it asks about the card as it stands BEFORE the write — sending work off today's board is the point of half these gestures. A board drawn from two listings counts both: the Triage grid with its drawer.

### The description of this surface

Every route under `/api/v1` except the index itself, with its body and the resources it answers, is described in [`api/openapi.yaml`](../api/openapi.yaml), and the server hands the same document out as JSON at `GET /api/v1/openapi.json`. It is written by hand and it is the contract: the server's own routes and request types are generated from it and so are the TypeScript client's types (`make generate` rebuilds both; `npm run generate` from `web/` does the TypeScript half alone), an agent can read the surface from it instead of being told it in prose, and the server's own tests hold their exchanges against it — the response always, the request when the server accepted it — so a route whose shape drifts fails the build rather than a client. Read it for what a route takes and answers; the rules that span routes are here.

Two routes are not generated, because a generated operation cannot be either. The index at `GET /api/v1` would be the path `/` under a server of `/api/v1`, which Go's mux turns into a subtree pattern that answers every unknown GET beneath it — the document leaves it out, and the server registers it by hand. The watch upgrades the connection instead of answering, so it needs the raw writer; the document describes it for clients and the generator skips it.

Every mutating request is **one action**: whatever it writes — a card and its review card, a column and every card under it — lands in one commit per touched repository, authored by the visitor, with `Aeman-Action`/`Aeman-Action-Id` trailers tying the commits together. Field writes that arrive in quick succession from the same visitor (a progress slider) coalesce into one commit with the final value.

`place` and `untriage` are the Triage board's own gestures and are **not** the same as `PATCH {"week": …}`: the patch writes the week and nothing else, while `place` also empties the working area for a week AHEAD and joins the team's sprint for the CURRENT one. MCP has both as tools of their own (`place_card`, `untriage_card`); `update_card week=` is still the bare patch, so an agent using it to schedule work for a later week has to clear the dates itself, or the card stands on today's board and in a future week at once.

A **capacity** — a team's or a person's — is a number somebody SET, never one the board worked out. The board does not read a capacity off its own record, because `doneAt` only goes back to the day it started keeping one and a median over a half-empty window reads as a fact while being an artefact of the record; the `derive-capacity` skill is that arithmetic, for a lead to run and write the answer back. A team's capacity carried four more fields until v0.37 — `week`, `client`, `internal` and `derived` — which nothing wrote and no client read.

Every team **has** a backlog and nothing declares it — the no-team group included, which is a team like any other here. It cannot be created, renamed or removed, so no card is ever left pointing at a shelf that has gone and parking can never be refused for want of one. Parking never changes a card's `team`; to move parked work to another team, send `team` and `parked` in the same patch — the team is applied first. The shelf has a create of its own: `POST /api/v1/views/backlog/cards` files the card there directly, with no dates and no sprint, rather than putting it in the strip and parking it a moment later.

A card's **column** obeys the domain closure from both sides, and a client narrowing a column picker cannot do it from `projectDomains` alone. The column must live in the repository that holds the card's FILE — which is usually the card's project, but not when a link outranks it: a subtask's file rides its parent, a review card's follows its original. For those, compare the card's own `status.domain` against the target column's repository, `metadata.epics[].domain` — which a multi-repository server names for every column, the primary's included, and a hand-built board may leave empty for the primary. It is the COLUMN's, never its project's: one project name may be declared in two repositories with its columns merged under a single entry, and then the two differ. Every act that could break the pair is refused with **422**: attaching or moving the card to such a column, grouping it under a parent elsewhere, pulling it back out, re-teaming it, or setting `reviewOf` — and equally moving the card whose file others FOLLOW, while any of them stands in a column of the repository being left.

### LIST selectors

`GET /api/v1/views/{view}/cards` reproduces the UI's views server-side — the board is the segment, and everything below narrows it:

- `/views/me/cards` — the caller's own **Me** board (their own cards in the active sprint), where everyone works day to day. Who-am-I is resolved server-side (session/`gh` login), so no `user` is needed; an explicit `?user=` still wins. A board is never guessed: the bare collection that defaulted to Me is gone.
- `/views/all/cards` — every card on the board (still honours the field/team filters).
- `/views/team/cards?team=platform&day=2026-07-02` — the Team grid (the lead view) for a team on a day; `team=` accepts a comma-separated set (`team=platform,marketing`) so the multi-team board loads in one request. Day defaults to today.
- `/views/me/cards?user=octocat&day=` — the Me day view for a specific user (empty user = the caller).
- `/views/triage/cards?team=platform&from=2026-08-31&weeks=6` — the **Triage board**: the cards standing in a window of weeks, people across and weeks down. `from` is the Monday the window opens on (defaulting to this one) and `weeks` how many it covers, that Monday included. The answer holds the cards placed in those weeks, the Project-board slots covering them, the process turns filed into them, and the debts owed in an earlier week that are still open — plus, in the same listing, the cards nobody has placed at all (`status.triage`), which is the strip the board shows beside the grid. A client must ask for every week it intends to DRAW: a card written into a week outside the window comes back in no listing, and a week ahead is on no day board either, so it would stand on no board at all.
- `/views/backlog/cards?team=platform` — what a team has **parked**: the cards on its shelf, in the order it is read (by hand first, then oldest first, so a shelf nobody has arranged still answers "what has been sitting here longest"). Omit `team` for every team's. These cards are in no week and on no day board, which is why they are a view of their own: the strip asks "when is this due", a shelf has already answered "not now".
- `/views/project/cards?project=cozystack` — the **Project board**: every card filed under one project's epic columns, all weeks at once (the client lays the weeks × epics table out itself). A card counts as filed by its own `epic` — a SUBTASK that carries one is delivered on that merit, not as a rider of its parent, because the parent commonly has no column at all (G57); a subtask without an epic belongs to no Project board and is not delivered. The PARENT of a delivered subtask rides along, column or not, so a client can name the slot it marks — it is not drawn (the grid draws what carries a column) and no field selector (`stage=`, `zone=`, `assignee=`, `focus=`) trims it away — it is delivered to be named, not to be drawn. Without `project=` it is every project, including columns that belong to none.
- `snapshot=1` — with a **past** `day` on a me/team view: the board **as it stood** when that day ended, instead of today's cards filtered by that day's dates. The same day is answered the same way at every door — `GET /api/v1/views/{view}/cards`, the MCP `list_cards`, an embedder through `boardservice.BoardOfDay`. Whether a day is over is asked **per team**: a team whose sprint has moved past it contributes what it held that evening (each such card carries `status.asOf`), a team still inside that sprint contributes its live cards, and a listing with no records at all is answered as today's board with no `asOf`. Ask `/board` and `/sprints` with the same selector — their pointers split the same way, and placing that day's cards under today's pointers drops nearly all of them. Every field is the day's own — a card finished since reads unfinished, one that changed team reads where it stood, one created since is absent — because the answer is the repositories' tree at the last commit of that day, not a replay over today's cards. The response carries `asOf` (that moment, RFC3339). Today and tomorrow have no snapshot and the flag is ignored there. A day the clone's history no longer reaches is **410 Gone** (the server first deepens on demand, bounded by `--history-max`); storage that keeps no history at all answers **501**. Nothing about it is writable — it is a record of a day, and the UI freezes the board while one is shown.
- A snapshot listing also carries **what the × took off that day**: the day's board is the tree that day ended with plus every card the day removed, each read from the commit that removed it — so a card finished at three and tidied away at five is on that day, done. (A card demoted rather than deleted, by a × pressed before this changed, is given back by its `leftAt` mark instead.) Today's board carries neither — the × is what takes a card off today.
- **Writing from a past day is refused.** A request made while a past day is on screen says so with the `X-Aeman-As-Of: <yyyy-mm-dd>` header, and a write to a card that day is over for is **409** — the day is a record, and the change would land on TODAY's card while the person is looking at a picture of a day that ended. A live card on the same mixed screen still writes, and so does a **create**: `POST /api/v1/views/{view}/cards` names its team rather than a card, and is judged by that team — while a sprint is open every day of it is still that team's to work, so a card can be added on the day the sprint began or any day since; only a team the day is over for is refused. A write that names neither a card nor a team (a carry-over, a roster change) is refused whenever any part of the view is a record. A request with no header is an ordinary write, as every other client makes. The UI sends the header for every request while it shows a past day, which is what turns a UI path that forgot into a loud failure instead of a silent one.
- Field selectors — `stage=`, `zone=`, `assignee=` — compose with a view or apply to all cards.
- `focus=true` — keep only cards workable right now (drops done, on-review and locked); the "what can I pick up now" filter.
- `reviews=true` — bring review cards into the view. On me/team it APPENDS each card's linked review, so a client rendering the reviewer badge has it without a second request; on **triage** it KEEPS the review cards the grid otherwise leaves out, each standing in the week its own dates fall in (a review has no week of its own — that belongs to the card it reviews) and one owed in a week gone by arriving in the current column as a debt. Off by default in both: an agent's Me list is not padded with review cards, and a grid full of open reviews is not a plan. The SPA asks for them on triage and folds them into a line at the foot of each cell ("+2 reviews"), so opening one costs no request.
- `fields=full` — complete cards with descriptions, for genuine bulk readers (analytics over card bodies). The default is the board-row shape: reading one card's body is `GET /cards/{uid}`, not a fatter list.
- On the me / all lists, `team=` filters by a comma-separated set (`team=marketing,portal`) matching any of them.

A `view=` in the QUERY is refused (400): the board moved into the path, and a request carrying one asks for a board it would not be answered from.

Every listing is the visitor's projection: cards in domains they cannot read are not there.

### Live updates: list + watch

Clients follow the Kubernetes list/watch pattern:

1. LIST: `GET /api/v1/views/{view}/cards` (+ `/sprints`) — the current state.
2. WATCH: `GET /api/v1/views/{view}/watch?client=<id>` — upgrade to a WebSocket; each text frame is one event:

```json
{ "type": "ADDED" | "MODIFIED" | "DELETED", "kind": "Card" | "Sprint" | "Ordering" | "Board" | "Load", "object": { ... } }
```

Apply Card events by `metadata.uid`; Sprint events replace a team's pointer; an Ordering event carries the full uid list to re-sort by. On reconnect, re-list to reconcile. Frames about a domain the visitor cannot read are never sent.

The optional `client` id keys **echo suppression**: send the same value in the `X-Aeman-Client` header on your own mutations and the server will not stream your own changes back on that watch connection (your optimistic state and the mutation responses already carry them).

**Scoped watch**: the board is the path segment, as it is for a listing, and the same narrowing parameters apply (`team=`, `day=`, `stage=`, ...); the subscription tracks that selection — a card entering it arrives as `ADDED` and one leaving it as `DELETED`, so a thin client can mirror a single view without knowing the board rules. Memberships are re-diffed when a sprint pointer moves and when the local day rolls over. `resources=cards,sprints,ordering` picks the kinds.

Deciding what a scoped view holds means building that view, so it is decided **once for a burst of changes** rather than once per card: a request that moves a team's whole backlog is answered with one frame per card a moment (~25 ms, stretched on a board where the fan-out is expensive) after its writes. Unscoped subscriptions still receive each change as it lands.

**Load frames**: what the board's PEOPLE are holding — `carrying`, `load`, `capacity` — is summed over cards from every team, so a client holding one view's cards can neither compute it nor learn it from the card events it receives: a size set on a card the tab is not showing still moves the number over its owner. Every card write and every capacity write therefore announces the people as a `MODIFIED` frame of kind `Load` carrying `{members: [...]}` — the same shape as `metadata.members`, to be merged in place. Coalesced for a burst exactly as Board frames are, and sent apart from them because these numbers move on every write while the roster hardly ever does.

**Board frames**: a change to the board's STRUCTURE — a project, a column, a deadline, a process or one of its tasks — cannot be expressed as a card event, so it arrives as a `MODIFIED` frame of kind `Board` carrying the whole board resource plus the process structure (apply it, no round trip needed). These frames are **coalesced**: one request that touches many cards announces the board once, a moment (~25 ms) after its changes, not once per card — a carry-over moving a team's whole backlog would otherwise repaint the board for every open tab hundreds of times over, and it did.

Changes that reach the repository from elsewhere — another aeman replica, a plugin committing directly — arrive the same way: the server fetches on its sync tick (`--sync-interval`, 15 s), reads exactly the cards the new commits touched, and streams them.

### Health

`GET /api/healthz` answers `{"status": "ok"}` and, in addition, what the storage has to say:

```json
{
  "status": "degraded",
  "unpushedAgeSeconds": 412,
  "cacheAgeSeconds": 9,
  "aliases": [{ "kind": "project", "name": "Docs", "domain": "closed", "id": "01JB…", "winner": "01JB…" }],
  "ghosts": [{ "id": "01JB…", "domain": "aeman-db", "current": "closed" }]
}
```

- `unpushedAgeSeconds` is the age of the oldest commit not yet pushed; past `--unpushed-warn` (5 min) the status is `degraded` — a push that cannot land must not be discovered a week later. Commits are never lost: they stay in the clone and are pushed when the remote is reachable again.
- `cacheAgeSeconds` is how long ago the cache was last known to be the remote — a full read, or a fetch that found nothing new. It should stay within the fetch interval; a number that keeps growing means the sync is not running, and the next visitor after a break pays for a blocking re-read.
- `aliases` names roster entries (teams, projects, processes) declared under the same name in two domains; the oldest wins, the others' cards still count. A maintainer merges them by hand.
- `ghosts` are cards left behind by a move that landed in the destination but whose source-side delete has not yet; maintenance removes them.

### Examples

```sh
# All cards, in board order
curl 'http://127.0.0.1:8765/api/v1/views/all/cards'

# The team grid for today
curl 'http://127.0.0.1:8765/api/v1/views/team/cards?team=platform'

# Create an urgent card assigned to octocat
curl -X POST 'http://127.0.0.1:8765/api/v1/views/team/cards' \
  -H 'Content-Type: application/json' \
  -d '{"title":"Fix the build","zone":"urgent","assignees":["octocat"]}'

# Bump readiness to 80%
curl -X PATCH 'http://127.0.0.1:8765/api/v1/cards/01JB4KA0M2P4R6T8V0X2Z4B6D8' \
  -H 'Content-Type: application/json' -d '{"progress":80}'

# Add a note
curl -X POST 'http://127.0.0.1:8765/api/v1/cards/01JB4KA0M2P4R6T8V0X2Z4B6D8/notes' \
  -H 'Content-Type: application/json' -d '{"text":"Deployed to staging"}'

# Preview a carry-over, then run it
curl -X POST 'http://127.0.0.1:8765/api/v1/sprints/actions/carry-over' \
  -H 'Content-Type: application/json' -d '{"team":"platform","dryRun":true}'
curl -X POST 'http://127.0.0.1:8765/api/v1/sprints/actions/carry-over' \
  -H 'Content-Type: application/json' -d '{"team":"platform"}'

# Plan a project in the closed domain: declare it there, give it a column,
# then file a card in that column — the card lands in the closed repository
curl -X POST 'http://127.0.0.1:8765/api/v1/projects' \
  -H 'Content-Type: application/json' -d '{"name":"cozystack","domain":"closed"}'
curl -X POST 'http://127.0.0.1:8765/api/v1/epics' \
  -H 'Content-Type: application/json' -d '{"name":"Auth","project":"cozystack"}'
curl -X POST 'http://127.0.0.1:8765/api/v1/views/project/cards' \
  -H 'Content-Type: application/json' \
  -d '{"title":"SSO for the console","epic":"Auth","project":"cozystack","week":"2026-08-24","dates":{"end":"2026-09-11"}}'
```

## Configuration

`aeman serve` and `aeman mcp` share the storage flags; each has an environment variable unless its row says otherwise, and the flag wins. The `--listen` and `--listen-insecure` rows belong to `aeman mcp` and to `aeman service`, which takes the storage flags too and bakes what they resolve to into the unit it writes.

| Flag | Environment | Default | Meaning |
| --- | --- | --- | --- |
| `--repo name=url` | `AEMAN_REPOS` (comma-separated) | — (required) | A domain of the board; repeatable, the primary first. The name is the domain's name on the API and the board's name for the primary. |
| `--forge` | `AEMAN_FORGE` | from the primary repository's host: `github.com` → `github`, a host containing `gitlab` → `gitlab`, else `github` unless `AEMAN_GITLAB_URL` is set | The forge that signs visitors in and answers who may read which repository: `github` or `gitlab`. |
| `--gitlab-url` | `AEMAN_GITLAB_URL` | `https://<host of the primary repository>` | Base URL of a self-hosted GitLab. |
| — | `AEMAN_GIT_TOKEN` | GitHub: `GITHUB_TOKEN`, `GH_TOKEN`, then the OS keychain (`aeman login`), then `gh auth token`; GitLab: `GITLAB_TOKEN`, then the OS keychain (`aeman login`), then `glab config get token --host <host>` | The server's own credential: fetch, push, membership checks, and resolving issue titles. Required in the OAuth mode. |
| — | `AEMAN_GIT_TOKEN_<NAME>` | `AEMAN_GIT_TOKEN` | One repository's own credential, named after its domain in `AEMAN_REPOS` — upper-cased, anything but a letter or a digit an underscore (`founders` → `AEMAN_GIT_TOKEN_FOUNDERS`). A board across two organisations holds one token per organisation; issue titles are resolved with the primary's. |
| `--data` | `AEMAN_DATA` | `/data` if it exists, else the user cache dir | Where the clones live (`<data>/repos/<name>`) and the session file. One process at a time: the PROCESS holds an exclusive lock on `<data>/lock` for as long as it runs — taken before the board is opened, and kept even by a start that could not open it and stays up (the app-not-installed setup page) — so a second `serve` or `mcp` on the same directory is refused at start. That needs a file system implementing locks, which a bind mount into a VM (9p) and some network mounts do not — those refuse the start naming the directory, so keep `--data` local. |
| `--history` | `AEMAN_HISTORY` | `2w` | How far back the history is loaded in the background after start-up. The cold start is a depth-1 clone; the log fills in behind it. |
| `--history-max` | `AEMAN_HISTORY_MAX` | `1y` | Cap for on-demand deepening when a card's log is cut by the horizon. |
| `--sync-interval` | `AEMAN_SYNC_INTERVAL` | `15s` | How often other replicas' and direct commits are fetched (and the week's process turns filed). |
| `--unpushed-warn` | `AEMAN_UNPUSHED_WARN` | `5m` | Age of the oldest unpushed commit that turns `/api/healthz` degraded. |
| `--committer` | `AEMAN_COMMITTER` | `aeman <aeman@localhost>` | The committer identity; also the author of the server's own actions (the sweep that files process turns, a schema migration). |
| `--author-email` | `AEMAN_AUTHOR_EMAIL` | `{login}@aeman` | How a visitor's login becomes the commit author's email. |
| `--listen` | `AEMAN_MCP_LISTEN` | `aeman mcp`: none, meaning stdio. `aeman service`: `127.0.0.1:8766` | The loopback address the daemon serves on. For `aeman mcp` it replaces stdio; for `aeman service install` it is the address baked into the unit, and for `aeman service status` the one to ask (the unit's own is read back when neither the flag nor the variable is set). |
| `--listen-insecure` | — | `false` | `aeman mcp` and `aeman service install` only: let `--listen` bind a non-loopback address. Nothing authenticates the endpoint, so this has to be typed; there is no environment variable for it. |

`aeman serve` adds `--addr` (default `127.0.0.1:8765`), `--open` and `--verbose`; `AEMAN_TZ` is the board's day time zone. The self-hosted OAuth mode is enabled by one client id/secret pair — `AEMAN_GITHUB_CLIENT_ID`/`AEMAN_GITHUB_CLIENT_SECRET` or `AEMAN_GITLAB_CLIENT_ID`/`AEMAN_GITLAB_CLIENT_SECRET`, never both — together with `AEMAN_BASE_URL` (required; the redirect URL registered at the forge is `<AEMAN_BASE_URL>/auth/callback`), `AEMAN_SCOPES` (default `repo` on GitHub, `read_user read_api` on GitLab), `AEMAN_SESSION_FILE` and `AEMAN_SESSION_KEY`; the server credential in this mode is `AEMAN_GIT_TOKEN` — or, on GitHub, an App that mints it: `AEMAN_GITHUB_APP_ID` with `AEMAN_GITHUB_APP_KEY`/`AEMAN_GITHUB_APP_KEY_FILE`. Registering the application at either forge is walked through in [deploy.md](deploy.md).

Bootstrapping: `aeman init --repo <url> [--title …]` writes an empty board (one commit) into an unborn repository on either forge — the URL is the HTTPS clone URL, e.g. `https://gitlab.com/<group>/<project>.git`; `serve` refuses an unborn remote and names that command. A repository written by a newer aeman (a higher `schema` in `board.yaml`) is refused at start-up; an older one is migrated in one commit.

### aeman login

`aeman login` puts the forge token in the operating system's secret store, so a local `aeman mcp` or `aeman serve` needs no credential in its environment or in an MCP client's configuration file. `aeman logout` takes it back out. Both take the same `--repo` / `--forge` / `--gitlab-url` flags every other command does, because those are what name the forge instance the token is for.

The token is never taken as an argument: `aeman login <token>` is refused rather than ignored, because by then the token is in the shell's history and in every `ps` listing. It is read hidden from a terminal, or as the first line of standard input when there is no terminal — `gh auth token | aeman login` works, and so does anything else that prints a token. It is verified with the forge before it is stored, and a rejected one is not written at all: an item holding a typo is worse than no item, because every later command finds it, stops looking, and fails on a credential nobody can see. What is printed on success is the login the forge gave back, which is the name the commits from that machine will carry.

The item is keyed by the forge's host — `github.com`, `gitlab.com`, `gitlab.example.org` — so one machine holds one token per forge instance and signing in to a self-hosted GitLab does not overwrite the github.com one. The forge is asked before anything is stored, so the command needs to reach it: on a machine whose VPN or proxy is not up, seed the credential through `GITHUB_TOKEN`/`GH_TOKEN` (or `GITLAB_TOKEN`) instead and run the login later. Any failure to store fails the command and names `GITHUB_TOKEN`/`GH_TOKEN` (or `GITLAB_TOKEN`), which cover the credential and the identity the way the keychain would, with whatever the store itself said kept after the advice; there is no plaintext-file fallback. The advice is the same for every failure on purpose, because the store cannot tell them apart: on macOS everything but a missing item arrives as the text `/usr/bin/security` printed, so a keychain locked over SSH and one that refuses a particular item look alike from here.

Two things to know on macOS. The item is written through `/usr/bin/security`, which puts it in the stable `apple-tool` access partition: that is what lets a rebuilt aeman still read what it stored, and the trade is that any process running as the same user can read it without a prompt. And the item lives in the login keychain, which is unlocked by logging in, so a service that reads it must run as a LaunchAgent in the user's session rather than as a LaunchDaemon.

The token does not travel on a command line. `security` is driven through its interactive mode and the write arrives on the tool's standard input, so the secret is not in the process list and not in what an agent recording process arguments would capture; it is base64-wrapped there so that arbitrary bytes survive, not to hide anything. The library falls back to a command-line argument where that line cannot carry the value: past what the tool reads per command, or for a service or account holding a newline or a NUL. Neither happens here, where the service is a fixed name and the account a forge host.

Coming from an older wrapper script that kept the token under its own service name:

```sh
security find-generic-password -s aeman-mcp -w | aeman login
security delete-generic-password -s aeman-mcp
```

## MCP server

`aeman mcp --repo name=url` starts a Model Context Protocol server on **stdio, or on a loopback HTTP daemon shared by every client on the machine** (`--listen 127.0.0.1:8766`), on its own clone of the board. In the self-hosted mode the same tool set is served over HTTP at `/mcp`. The tools are a one-to-one projection of the HTTP API — same resources, same actions, same semantic zone names, item ids called `uid`:

| Tool | Purpose |
| --- | --- |
| `get_board` | The board: team roster, the Project board's structure (`metadata.projects`, `metadata.epics`, `metadata.deadlines`), the people and the domains. |
| `list_cards` | LIST with the same selectors (`view`, `team`, `day`, `user`, `stage`, `zone`, `assignee`, `focus`, `reviews`, `from` and `weeks` for the Triage window — a card scheduled past it is in no listing at all — and `snapshot`, a past `day` as it stood, see the selectors above). Returns board ROWS (no descriptions; `status.links` carries the extracted refs). `title=<substring>` resolves a card someone mentioned by name to its uid in one cheap call; `full=true` opts a bulk reader into complete cards. No view defaults to your own Me board (who-am-i resolved server-side); `view=all` is the whole board, `view=team` the lead view. |
| `get_card` / `list_notes` / `list_links` | One card IN FULL — the detail pane, and the way to read a body after a `list_cards` row; its notes; its description links (GitHub refs resolved with titles). |
| `list_log` | The card's activity feed from the repository's history: events (stage/progress/review/week changes with actor) + notes, one chronological list — read a card's delta instead of asking for morning reports. `truncatedBefore` says when the loaded history is cut. |
| `create_card` | Create the card a BOARD makes — `view` says which: `me` (the default) files it on the person the agent is acting for, for the day, in their sprint, in the unplanned band (the only one that board adds in); `team` files it in the Unassigned column unless an assignee is named; `triage` for a `week` and no day; `backlog` on the team's shelf; `project` under a column. Each refuses the fields it does not own and names the one it refused. A title that is only a GitHub issue/PR URL is auto-filled from that item. An optional `description` gives the card its body at create (with a URL title the link is kept — appended to an explicit body that does not already name that item, in either spelling). Passed with `reviewOf` it is the SHARED body of the review pair, so a create that would overwrite a body already standing on the card being reviewed is refused rather than made. |
| `update_card` | The PATCH: only provided fields apply, empty clears. The `description` is the card's shared body — and the place for reference links: include full URLs of related open PRs/issues in free form (encouraged); they are surfaced on the card and GitHub refs resolve to live titles/states (`list_links`). |
| `delete_card` / `remove_card` | Hard delete; the smart × — `remove_card` takes the same `intent` the REST action does (`unassign`, `off-board`, or omitted to let the gesture decide). It also takes a `view`: an agent is standing nowhere by default, and naming a board is how it asks to be held to one — a card that board does not draw is then refused, as it is for a person looking at that screen. |
| `place_card` / `untriage_card` | The Triage board's drop and its opposite: schedule the card for a week (its Monday), or take the week back and return it to the strip. |
| `finished_earlier` | A day board's answer for work done in the sprint before this one and marked done only now: it goes back to the sprint it was worked in. |
| `list_sprints` | When each team's sprint began — the current pointer and the one before it. Every date rule on the board is reckoned against these. |
| `list_day_logs` | One day's feed for many cards at once (`uids`, at most 200, and a `day`): what a day board shows, at a fraction of a log per card. |
| `reorder_teams` / `delete_team` | The shared team order (it moves the hidden sprint-state cards, so every device sees it); delete a team's pointer, refused while any card still names it. |
| `reorder_epics` | One project's column order, left to right, shared by everyone. |
| `set_sprint_state` | Set a team's sprint pointers by hand — the repair tool. `carry_over` is what advances a sprint in the normal course of things. |
| `move_card` / `defer_card` | Reorder; push the scheduled day ahead. |
| `send_to_review` / `remove_reviewer` | The review-card cycle (send reassigns when a review card exists). |
| `mirror_card` / `unmirror_card` | Show the card in a second column ({uid, project, epic}) — the same card, one file and one log, the card's own repository only — and take one away. |
| `remove_from_project` | The Project board's ×: a mirror goes; the home hands over to its first mirror; the last column keeps only a worked card, as a working-area orphan, and deletes the rest — never a SUBTASK, whose other home is its parent: there the × only takes the column away. |
| `carry_over` | Advance a team's sprint and carry its unfinished day cards forward (`dryRun` reports the counts). There is no weekly counterpart: the server files each week's process turns by itself, and a card left open past the week it was owed in is a debt that stays where it was and shows as overdue. |
| `add_note` / `edit_note` / `delete_note` | The note thread. |
| `list_processes` | The Process tab in one call: processes, tasks, and each task's history (done / open / late per iteration). |
| `add_process` / `delete_process` / `rename_process` | The process roster inside a project; `add_process` takes an optional `domain`. |
| `set_process_project` / `set_process_paused` | Move a process between projects; pause it (it files no turns) or resume it. |
| `add_process_task` / `update_process_task` / `delete_process_task` | What a process iterates on: title, body, cycle (`week` / `2weeks` / `month` / `quarter`), start, team, owner, `accumulate`. |
| `add_deadline` / `delete_deadline` / `move_deadline` | A project's deadline lines: mark a week, clear it, drag one to another week (two of the same project on one week become one). |
| `add_project` / `delete_project` / `rename_project` / `reorder_projects` | The project roster: declare one (it may start empty; optional `domain`), delete an EMPTY one, rename one (its columns and cards follow), set the chip order. |
| `rename_team` | Rename a team in place (`team`, `to`); its cards and process tasks follow, a taken name is refused. |
| `set_capacity` / `set_team_capacity` | The points a week a PERSON (`login`) or a TEAM (`team`) gets through — what a load and a week's plan are weighed against. `capacity=0` takes a number back; the board derives none in its place, and the `derive-capacity` skill is the arithmetic for arriving at one. The person tool answers the whole board; the team tool answers that team's sprint resource, where a team's number lives. |
| `add_epic` / `delete_epic` / `rename_epic` / `set_epic_project` | The columns of a project, each named by the `(project, epic)` pair: `add_epic` requires `project=`, `delete_epic` refuses a column with cards, `rename_epic` rewrites the column and its cards, `set_epic_project` moves one between projects (an empty target detaches it). |

The tools act on the configured board; a `board` argument, if a client passes one, is ignored. Changes made by agents are committed and streamed to every open board over the watch, like any other write; the stdio server pushes what accumulated when the client closes the pipe.

### Flags and environment

`aeman mcp` takes the storage flags of the configuration table above (including `--forge` and `--gitlab-url`) plus `--verbose`, `--listen` and `--listen-insecure`. With `--listen host:port` (or `AEMAN_MCP_LISTEN`) the process serves Streamable HTTP at `/mcp` instead of speaking stdio — one daemon every client shares. `GET /healthz` answers `{"status", "unpushedAgeSeconds"}`, turning `degraded` past `--unpushed-warn` the way `aeman serve`'s `/api/healthz` does: nobody watches a daemon, so a push that stopped landing has to be visible somewhere. It is the stuck write that shows, not the credential: with nothing unpushed the age is zero and the status is `ok`, however broken the credential is. The push credential is `AEMAN_GIT_TOKEN`, else `GITHUB_TOKEN`/`GH_TOKEN` (GitHub) or `GITLAB_TOKEN` (GitLab), else the OS keychain written by [`aeman login`](#aeman-login), else the forge CLI's own token (`gh auth token` / `glab config get token --host <host>`). The actor is the person that credential belongs to, `AEMAN_GIT_TOKEN` excepted: that one names a push credential only.

Logs go to stderr, never stdout, so they never corrupt the JSON-RPC stream.

### Configuring it in an MCP client

Run [`aeman login`](#aeman-login) once first, so the configuration holds no credential:

```sh
aeman login --repo https://github.com/acme/aeman-db.git
```

Claude Code / Claude Desktop style config:

```json
{
  "mcpServers": {
    "aeman": {
      "command": "aeman",
      "args": ["mcp", "--repo", "aeman-db=https://github.com/acme/aeman-db.git"]
    }
  }
}
```

Or add it from the command line:

```sh
claude mcp add aeman -- aeman mcp --repo aeman-db=https://github.com/acme/aeman-db.git
```

The same for a board on GitLab — the forge follows the repository URL's host, so only the URL changes; a self-hosted GitLab whose host does not contain `gitlab` also needs `AEMAN_GITLAB_URL` (or `--forge gitlab`), on the login as well as on the server:

```json
{
  "mcpServers": {
    "aeman": {
      "command": "aeman",
      "args": ["mcp", "--repo", "aeman-db=https://gitlab.com/acme/aeman-db.git"]
    }
  }
}
```

Without a stored token the server falls back to the forge CLI's own (`gh auth token` / `glab config get token --host <host>`), so an authenticated `gh` or `glab` is enough for local use; `AEMAN_GIT_TOKEN` in the `env` block still overrides both.

#### Shared daemon

Each client starting its own `aeman mcp` child means a clone per client, and a data directory takes one process — so with more than one client the second is refused. Run one daemon instead and point every client at it:

```sh
aeman service install --repo aeman-db=https://github.com/acme/aeman-db.git --listen 127.0.0.1:8766
claude mcp add --transport http aeman http://127.0.0.1:8766/mcp --scope user
```

The unit carries no credential — it is a plain file — so the daemon finds one the way `aeman mcp` does, and the simplest answer is to run [`aeman login`](#aeman-login) before installing: the daemon reads the same OS keychain, once, at start. Logging in afterwards reaches nothing until the daemon is restarted (`aeman service uninstall && aeman service install`), which is also what a rotated token needs; a GitHub App is the exception, since its installation token is minted per request. An environment variable set in the installing shell cannot travel with it, and `aeman service install` names what it had to leave behind: `AEMAN_GIT_TOKEN`, a per-repository `AEMAN_GIT_TOKEN_<NAME>`, a GitHub App (`AEMAN_GITHUB_APP_ID`), or a credential embedded in a `--repo` URL. Two ways to keep one: give the daemon a credential it can find for itself — the keychain, or the forge CLI — that reaches every repository, or skip the unit and run `aeman mcp --listen 127.0.0.1:8766` from a shell that has the variables. Starting `aeman mcp` per client is not one of them: the second client is refused.

The unit's label is fixed, so one machine runs one aeman daemon: a second `aeman service install` for another board is refused, and that board wants its own `--data` and a hand-run `aeman mcp --listen` on another port.

`aeman service install` writes a LaunchAgent (macOS) or a systemd user unit (Linux) that starts the daemon at login and restarts it if it dies; `aeman service status` says whether the unit is installed, whether the manager is running it, and whether it answers; `aeman service uninstall` stops it and removes the unit. `aeman service` is not supported on Windows — run `aeman mcp --listen 127.0.0.1:8766` yourself.

The stdio form still works and is still the right one for a single client, restarts included: a start gives a predecessor a few seconds to finish its exit drain, so closing and reopening a client does not meet its own outgoing process. What does not work is both at once on one `--data`: whichever starts second is refused with the message that names this daemon. The same applies to running the board UI and MCP together on one machine — `aeman serve` mounts `/mcp` in the self-hosted OAuth mode only, so there is no single local process that serves both. Give them separate `--data` directories, which means two clones of the board and two pushes, or run whichever of the two you actually need.

Nothing authenticates `/mcp`, so it binds loopback only; `--listen-insecure` is the deliberate exception.

The loopback boundary covers the browser, for `/mcp`. A page the person opens can post to `127.0.0.1`, so a cross-origin request there is refused before a session exists, and `/mcp` refuses a request whose `Host` is not loopback (the SDK's rebinding check). `/healthz` is a plain GET that passes the origin check and answers the same two fields to any origin — it carries no secret and performs no action.

It does not cover the neighbours. Loopback is shared by every account on the host, and anything that can open a socket to the port performs any board action under the daemon's identity — the origin check does not apply to a caller that sends no `Origin`, which is every non-browser client. Before the daemon nothing listened, and another local user had no route to your credential; with it running, they have one. **The daemon is for a machine whose accounts you trust.** On a shared host stay with `aeman mcp` over stdio, which only the process that started it can reach; where one account wants several clients that means one at a time, or a `--data` each.
