---
name: derive-capacity
description: Work out how many points a week each person on the aeman board actually gets through, from the cards they have closed, and set it as their capacity after the lead confirms. Use when a team lead asks about throughput, weekly capacity, how much a person or a team can take, or wants the numbers over the columns filled in.
---

# Derive a capacity

A person's CAPACITY on the aeman board is the points a week they get through. The board measures their load against it — "21/40" over the column, red past it — and it is the only number a lead has for saying "this week does not fit".

The board never derives it. It could, and this skill is that arithmetic — but a number a board prints beside a name reads as a fact, and this one is only as good as the record it comes from. So it is derived HERE, by somebody who can see how thin the record is, shown to the lead, and written back as their judgement.

## Before anything: is the record long enough?

`doneAt` is only written from the day a board started keeping it. A window that is empty of RECORDS while being full of WORK produces a median far under the truth, and nothing on screen says so.

1. `list_cards view=all stage=done` and read `status.doneAt` on every card. Find the earliest one.
2. Compare it against the four complete weeks before this one (Monday to Monday; this week never counts — it is not over).
3. If the earliest `doneAt` is inside that window, the record does not cover it. **Say so first, in the answer, before any number**: how many complete weeks are actually covered, and that everything below is a floor, not a capacity. Two covered weeks is worth showing as "at least N"; less than two is not worth deriving at all — tell the lead to set the numbers from what they know and come back in a month.

Also check how much is SIZED: `spec.size` on the closed cards. An unsized card weighs M, so a board where few cards are sized gives you a count of cards in M-equivalents, not a weight of work. If most closed cards are unsized, run `size-cards` over the window first, or say plainly that the number is a card count in disguise.

## The arithmetic

Over the last four complete weeks, per person:

- Take the cards with a `status.doneAt` in the window, on that person (`spec.assignees[0]` — the first assignee owns the card; a card is not counted twice).
- Leave out: subtasks (`spec.parent` set — they are weighed on their parent), review cards (`spec.reviewOf`), the board's own state cards, and anything on a personal domain. This is the team's work, not everything that moved.
- Weigh each card: S=1, M=2, L=4, XL=8, and a card with no size weighs M. An umbrella — a card with sized children — weighs its children, once, not itself.
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

A team's capacity is not set anywhere: it is the sum of its people's, with somebody who works across two teams split between them in proportion to what they closed in each. So the way to fix a team's number is to fix a person's.

## What this number is not

- It is not a target. It is what happened, and the point of writing it down is to notice a week that does not fit — before the week, not after it.
- It is not comparable between people whose work is shaped differently. Somebody paid per card closes many small ones; somebody carrying one migration closes almost nothing for three weeks and then a lot. The median helps with the second and not at all with the first — say so rather than lining people up by the number.
- It is not stable. Re-derive it when the shape of somebody's work changes, not weekly: a number that moves every Monday stops being something anyone stands behind.
