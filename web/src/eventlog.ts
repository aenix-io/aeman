// Human labels for card activity events (see board.Event server-side).

import type { CardEvent } from "./providers/types";

/** eventLabel renders an event as a short human line for the log feeds. */
export function eventLabel(e: CardEvent): string {
  const from = e.from ?? "";
  const to = e.to ?? "";
  switch (e.kind) {
    case "created":
      return "created";
    case "progress":
      return `progress ${from || "0"}% → ${to || "0"}%`;
    case "stage":
      return `status ${from || "—"} → ${to || "—"}`;
    case "assignee":
      return to ? `assigned to @${to}` : `unassigned (was @${from})`;
    case "team":
      return `team ${from || "—"} → ${to || "—"}`;
    case "epic":
      return `column ${from || "—"} → ${to || "—"}`;
    case "mirror":
      return to ? `mirrored to ${to}` : `no longer mirrored to ${from}`;
    case "process":
      return to ? `tied to the process ${to}` : `untied from the process ${from}`;
    case "zone":
      return `zone ${from || "—"} → ${to || "—"}`;
    case "review-sent":
      return from && from !== to
        ? `review moved @${from} → @${to}`
        : `sent to review → @${to}`;
    case "review-passed":
      return from ? `review passed by @${from}` : "review passed";
    case "reviewer-removed":
      return from ? `reviewer @${from} removed` : "reviewer removed";
    // The weekly plan is gone, but its entries stay in the log: a commit
    // already written is history, and history that renders as a blank line
    // is worse than history that names something the board no longer has.
    case "plan-taken":
      return to ? `taken into work by @${to}` : "taken into work";
    case "plan-added":
      return `added to the weekly plan (${to})`;
    case "plan-released":
      return "released from the weekly plan";
    case "dates":
      return `dates ${from || "—"} → ${to || "—"}`;
    case "sprint":
      return `sprint ${from || "—"} → ${to || "—"}`;
    case "week":
      return `week ${from || "—"} → ${to || "—"}`;
    case "plan-band":
      return `weekly-plan band ${from || "—"} → ${to || "—"}`;
    case "review-round":
      return `review round ${from} → ${to} (reset to 0%)`;
    case "parent":
      return to
        ? `grouped under «${to}»`
        : from
          ? `ungrouped (was under «${from}»)`
          : "ungrouped";
    case "subtask":
      return to ? `subtask «${to}» attached` : `subtask «${from}» detached`;
    default:
      return `${e.kind} ${from} → ${to}`.trim();
  }
}
