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
    case "plan-taken":
      return to ? `taken into work by @${to}` : "taken into work";
    case "plan-added":
      return `added to the weekly plan (${to})`;
    case "plan-released":
      return "released from the plan";
    case "dates":
      return `dates ${from || "—"} → ${to || "—"}`;
    case "sprint":
      return `sprint ${from || "—"} → ${to || "—"}`;
    case "week":
      return `plan week ${from || "—"} → ${to || "—"}`;
    case "plan-band":
      return `plan band ${from || "—"} → ${to || "—"}`;
    case "review-round":
      return `review round ${from} → ${to} (reset to 0%)`;
    default:
      return `${e.kind} ${from} → ${to}`.trim();
  }
}
