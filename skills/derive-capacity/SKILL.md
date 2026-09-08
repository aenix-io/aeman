---
name: derive-capacity
description: Work out how many points a week each person on the aeman board actually gets through, from the cards they have closed, and set it as their capacity after the lead confirms. Use when a team lead asks about throughput, weekly capacity, how much a person or a team can take, or wants the numbers over the columns filled in.
---

# Derive a capacity

A person's CAPACITY on the aeman board is the points a week they get through. The board measures their load against it — "21/40" over the column, red past it — and it is the only number a lead has for saying "this week does not fit".

The board never derives it. It could, and this skill is that arithmetic — but a number a board prints beside a name reads as a fact, and this one is only as good as the record it comes from. So it is derived HERE, by somebody who can see how thin the record is, shown to the lead, and written back as their judgement.

## Before anything: is the record long enough?

`doneAt` is only written from the day a board started keeping it. A window that is empty of RECORDS while being full of WORK produces a median far under the truth, and nothing on screen says so.

1. `list_cards view=all stage=done` and read `status.doneAt` on every card. Find the earliest one. (Done is DERIVED from progress, never stored on a card — the selector asks the board's own question, so this is the one place `stage=done` means anything.)
2. Compare it against the four complete weeks before this one (Monday to Monday; this week never counts — it is not over).
3. If the earliest `doneAt` is inside that window, the record does not cover it. **Say so first, in the answer, before any number**: how many complete weeks are actually covered, and that everything below is a floor, not a capacity. Two covered weeks is worth showing as "at least N"; less than two is not worth deriving at all — tell the lead to set the numbers from what they know and come back in a month.

Also check how much is SIZED: `spec.size` on the closed cards. An unsized card weighs M, so a board where few cards are sized gives you a count of cards in M-equivalents, not a weight of work. If most closed cards are unsized, run `size-cards` over the window first, or say plainly that the number is a card count in disguise.

## The arithmetic

Over the last four complete weeks, per person:

- Take the cards with a `status.doneAt` in the window, on that person (`spec.assignees[0]` — the first assignee owns the card; a card is not counted twice).
- Leave out: subtasks (`spec.parent` set — they are weighed on their parent), review cards (`spec.reviewOf`), the board's own state cards, and anything on a personal domain. This is the team's work, not everything that moved.
- Weigh each card: S=1, M=2, L=4, XL=8; a card with no size weighs M, and an unsized REVIEW card weighs S. An umbrella — a card with sized children — weighs its children, once, not itself.
- Group by the Monday of `doneAt`. That gives up to four weekly totals.
- **The capacity is the MEDIAN of the weeks the person closed something in.** Weeks with nothing closed are skipped, not counted as zero: a week off is not a slow week, and a number that remembers somebody's holiday is wrong the moment they come back. The median, not the mean, so one enormous week — an umbrella closed all at once — does not become the expectation.

Two weeks left after skipping means the median is their average; one week means the number is that week. Say which, per person: "median of 3 weeks" and "one week only" are different claims and the lead should see which one they are being handed.

## What to show

One row per person, ordered by capacity:

`login — capacity — the weekly totals it came from — how many weeks were skipped and why (nothing closed / no record) — share of closed points that was unsized`

Then STOP. The lead reads it and says which numbers stand. Expect them to overrule some of it, and they are right to: the record does not know that somebody was on holiday, joined last month, is paid per card rather than per week, or spends half their week on another team. A person whose row you cannot defend is a person to leave unset — no number at all is honest, a wrong one is not.

## Writing it back

For each number the lead confirmed: `set_capacity login=<login> capacity=<points a week>`.

`capacity=0` takes a number back — the board then draws that person's load alone, with nothing to measure it against. Use it when the lead says they no longer stand behind a number; never as a way of saying "recompute", because nothing recomputes.

## A team's number

A team has a capacity of its own — `set_team_capacity team=<key> capacity=<points a week>` — and the board derives it no more than it derives a person's. It is the whole of the team's `capacity:` block — the cards-a-week limit that used to sit beside it was removed (B7), so there is no second number to confuse it with.

Work it out in three steps, and say which of them you had to guess at:

1. **Add up the people.** The team's members and their capacities (`get_board` `metadata.members`), which you have just derived or which a lead has set.
2. **Take only the share of each person the team actually has.** Somebody who works across teams is not wholly anyone's. Split them by where their OPEN work is — `list_cards view=all` grouped by `spec.team` for that person, weighed the same way — rather than by what they closed: the closed record is only as deep as `doneAt` goes, and one busy week can hand a person's whole number to a team they barely touched. Say the split out loud in the table; it is the part a lead is most likely to correct, because they know that somebody is on portal this month whatever the cards say.
3. **Take off what arrives unplanned.** A week planned to the last point cannot absorb the work that turns up during it. Measure it: of the points the team closed in the window, the share that arrived unasked or had to be done that day — `spec.zone` is `unplanned` or `urgent` on the wire (the board's own yellow and red; `planned` and `niceToHave` are the other two) — on the production board that was a third for the engineering teams and two fifths for the portal team. A team's plannable week is its capacity less that share. If the window is too thin to measure, say so and leave the number at the raw sum, flagged.

The result is one number per team, shown to the lead and written back only where they agree.

A person's number and their team's are not kept in step by the board: change somebody's capacity and no team's number moves. That is the price of both being somebody's word. Re-derive the teams when the people change.

## What this number is not

- It is not a target. It is what happened, and the point of writing it down is to notice a week that does not fit — before the week, not after it.
- It is not comparable between people whose work is shaped differently. Somebody paid per card closes many small ones; somebody carrying one migration closes almost nothing for three weeks and then a lot. The median helps with the second and not at all with the first — say so rather than lining people up by the number.
- It is not stable. Re-derive it when the shape of somebody's work changes, not weekly: a number that moves every Monday stops being something anyone stands behind.
