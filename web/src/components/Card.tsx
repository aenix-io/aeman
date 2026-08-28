import { useMemo, useRef, useState } from "react";
import type { Card as CardModel, StageKey } from "../providers/types";
import { STAGES, STAGE_ORDER, DEFAULT_BAR_COLOR, isInProgress } from "../stages";
import { snapProgress } from "../progress";
import { teamColor, teamInitial } from "../avatar";
import { displayName, type Avatars, type Names } from "../users";
import { Avatar } from "./Avatar";
import { addDays, daysSince, localDateIso, todayIso, mondayOf } from "../date";
import { Dropdown } from "./Dropdown";
import { extractLinks, type CardLink } from "../links";
import { effectiveBand } from "../weekly";
import { recurrenceCycles, recurrenceLabel, recurrenceTitle } from "../personal";
import { RangeCalendar } from "./RangeCalendar";

// ageColor fades the age badge from light grey (fresh) to maroon-red by ~10 days.
function ageColor(days: number): string {
  const t = Math.min(1, days / 10);
  const from = [176, 181, 189]; // light grey
  const to = [140, 20, 40]; // maroon-red
  const ch = (i: number) => Math.round(from[i] + (to[i] - from[i]) * t);
  return `rgb(${ch(0)}, ${ch(1)}, ${ch(2)})`;
}

interface CardProps {
  card: CardModel;
  selected: boolean;
  onSelect: (card: CardModel) => void;
  onProgress: (card: CardModel, value: number) => void;
  onDelete: (card: CardModel) => void;
  /** The board will put its own question to the user, so the card must not
   *  ask first. */
  boardAsks?: boolean;
  onStage: (
    card: CardModel,
    stage: StageKey | null,
    recurrence?: "" | "week" | "month",
  ) => void;
  /** Pick the implicit "In Progress" status: clears the stage and clamps the
   *  card's progress into [10, 90]. */
  onInProgress: (card: CardModel) => void;
  onOpen: (card: CardModel) => void;
  /** Reassign the card's team / person from the avatar menu (when provided). */
  teams?: string[];
  people?: string[];
  /** Who may review this card — the people who can read its repository
   *  (see reviewerCandidates). Falls back to `people` when omitted. */
  reviewers?: string[];
  /** Avatars by login (the board roster); a login without one shows initials. */
  avatars?: Avatars;
  /** Display names by login (the board roster); a login without one is shown
   *  as is. Labels and tooltips only — the login stays the identifier. */
  names?: Names;
  /** The card's repository, shown only when the board spans several and the
   *  card is outside the primary (see cardDomainBadge). */
  domainBadge?: string | null;
  onSetTeam?: (card: CardModel, team: string | null) => void;
  onSetAssignee?: (card: CardModel, login: string | null) => void;
  /** This card has a linked review card; deleting it cascades (the board owns
   *  the combined confirmation, so the card skips its own delete prompt). */
  hasLinkedReview?: boolean;
  /** Assignees of the linked counterpart card — the reviewer(s) on an original
   *  under review, the implementer(s) on a review card. */
  counterpartAssignees?: string[];
  /** Manage the linked review card from the counterpart menu: a login reassigns
   *  the review card, null deletes it. Only when this card has a linked review. */
  onSetReviewAssignee?: (card: CardModel, login: string | null) => void;
  /** The day the board is showing; the age badge is measured up to it. */
  asOf?: string;
  /** Edit the card's start/end dates from the age badge (when provided). */
  onSetDates?: (card: CardModel, start: string | null, end: string | null) => void;
  /** Defer the card N days ahead of today (or of its already-deferred slot);
   *  the server computes the target day, sprint untouched. */
  onDefer?: (card: CardModel, days: number) => void;
  /** Plan-card mode: the age-badge editor moves the plan week instead of dates. */
  weekMode?: boolean;
  onSetWeek?: (card: CardModel, week: string | null) => void;
  /** Dim the card's team avatar to 50%. Set unless this card is the selected
   *  team, so only the selected team's avatars stay at full opacity. */
  dimAvatar?: boolean;
  /** A personal-board card: its default recurrence turns with the day, not
   *  the sprint, and the menu says so. */
  personal?: boolean;
  /** Resolve the card's description links server-side (GitHub issue/PR refs
   *  get their titles). The menu falls back to the local extraction. */
  onLoadLinks?: (card: CardModel) => Promise<CardLink[]>;
  /** Logins of teammates whose Me view has this card selected right now: the
   *  card highlights and their avatars hang off its right border. */
  selectedBy?: string[];
  /** Number of subtasks grouped under this card (drives the expand arrow and
   *  the derived progress bar). */
  subCount?: number;
  /** Whether the subtask list under the card is expanded. */
  expanded?: boolean;
  onToggleExpand?: (card: CardModel) => void;
  /** Add a subtask (shown as a hover + when the card has none yet). */
  onAddSubtask?: (card: CardModel) => void;
  /** A drag is hovering this card's middle band: dropping groups the dragged
   *  card as a subtask, and the card highlights as the target. */
  groupTarget?: boolean;
}

const SEGMENTS = 10;

/** barColor is the fill colour for the progress segments, driven by stage. */
function barColor(stage?: StageKey): string {
  return stage ? STAGES[stage].color : DEFAULT_BAR_COLOR;
}

/** Card is a compact single-row item shared by the Me and Team boards. */
export function Card({
  card,
  selected,
  onSelect,
  onProgress,
  onDelete,
  boardAsks,
  onStage,
  onInProgress,
  onOpen,
  teams,
  people,
  reviewers,
  avatars,
  names,
  domainBadge,
  onSetTeam,
  onSetAssignee,
  hasLinkedReview,
  counterpartAssignees,
  onSetReviewAssignee,
  asOf,
  onSetDates,
  onDefer,
  weekMode,
  onSetWeek,
  dimAvatar,
  personal = false,
  onLoadLinks,
  selectedBy,
  subCount,
  expanded,
  onToggleExpand,
  onAddSubtask,
  groupTarget,
}: CardProps) {
  // Done is derived, not stored: a card with no stage at 100% renders as done
  // (legacy cards with a stored Done option still count too).
  const doneish =
    card.stage === "done" || (!card.stage && (card.progress ?? 0) >= 100);
  const rawValue = doneish ? 100 : card.progress ?? 0;
  // Locked / on-review cards are clamped to a 10–90% band, so they always show
  // the stage colour and never read as complete.
  const value =
    card.stage === "locked" || card.stage === "review"
      ? Math.min(90, Math.max(10, rawValue))
      : rawValue;
  const fill = barColor(doneish ? "done" : card.stage);

  const [menuOpen, setMenuOpen] = useState(false);
  const [recOpen, setRecOpen] = useState(false);
  // The cycle submenu opens to the right; near the viewport edge it flips
  // left so it never clips off-screen.
  const [recLeft, setRecLeft] = useState(false);
  const [copied, setCopied] = useState(false);
  const [dragValue, setDragValue] = useState<number | null>(null);
  const [assignOpen, setAssignOpen] = useState(false);
  const [personInput, setPersonInput] = useState("");
  const [reviewerInput, setReviewerInput] = useState("");
  const menuRef = useRef<HTMLDivElement | null>(null);
  const assignRef = useRef<HTMLDivElement | null>(null);
  const barRef = useRef<HTMLDivElement | null>(null);
  const [datesOpen, setDatesOpen] = useState(false);
  const [linksOpen, setLinksOpen] = useState(false);
  const [resolvedLinks, setResolvedLinks] = useState<CardLink[] | null>(null);
  const linksRef = useRef<HTMLDivElement | null>(null);
  // Summary rows carry server-derived refs instead of the description;
  // parsing the body is the fallback for cards that arrived in full (watch
  // frames, mutation acks) before any refs field existed.
  const localLinksSrc = card.linkRefs;
  const localLinks = useMemo(
    () => localLinksSrc ?? extractLinks(card.description ?? ""),
    [localLinksSrc, card.description],
  );
  const hasGitHubRefs = localLinks.some((l) => l.kind !== "link");
  const toggleLinks = (e: React.MouseEvent) => {
    e.stopPropagation();
    const opening = !linksOpen;
    setLinksOpen(opening);
    // Resolve titles lazily on open; the local list shows meanwhile.
    if (opening && hasGitHubRefs && onLoadLinks) {
      void onLoadLinks(card)
        .then((links) => setResolvedLinks(links))
        .catch(() => undefined);
    }
  };
  const shownLinks = resolvedLinks ?? localLinks;
  const [startVal, setStartVal] = useState("");
  const [endVal, setEndVal] = useState("");
  const ageRef = useRef<HTMLDivElement | null>(null);

  const canAssign = Boolean(onSetTeam || onSetAssignee);
  const canEditDates = weekMode ? Boolean(onSetWeek) : Boolean(onSetDates);

  // While dragging the handle, show the snapped drag value; otherwise the card's.
  const shown = dragValue ?? value;
  // Snap the visible fill to the 10% grid, but never read a started card as empty
  // nor a sub-100 card as full: an off-grid value the slider can't produce (e.g.
  // 3 or 95 set via MCP/API) in (0,10) shows one segment and in (90,100) shows
  // nine — only a true 0 and 100 hit the extremes.
  const dispPct =
    shown <= 0
      ? 0
      : shown >= 100
        ? 100
        : Math.min(90, Math.max(10, Math.round(shown / 10) * 10));
  const filled = dispPct / 10;

  // Where a pointer at clientX lands on the 10% grid. review/locked cards are
  // clamped to 10–90%; other cards span 0–100% (0% clears).
  const valueAt = (clientX: number): number => {
    const rect = barRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0) {
      return value;
    }
    return snapProgress(
      (clientX - rect.left) / rect.width,
      card.stage === "review" || card.stage === "locked",
    );
  };

  // Progress moves only by dragging the handle. The bar itself stays inert on
  // purpose: it runs the full width of the card, and a stray press while
  // dragging cards around would silently rewrite someone's progress.
  const onHandleDown = (e: React.PointerEvent<HTMLDivElement>) => {
    e.stopPropagation();
    e.preventDefault();
    e.currentTarget.setPointerCapture(e.pointerId);
    setDragValue(value);
  };

  const onHandleMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (dragValue === null) {
      return;
    }
    const snapped = valueAt(e.clientX);
    if (snapped !== dragValue) {
      setDragValue(snapped);
    }
  };

  const onHandleUp = (e: React.PointerEvent<HTMLDivElement>) => {
    if (dragValue === null) {
      return;
    }
    e.currentTarget.releasePointerCapture(e.pointerId);
    const final = dragValue;
    setDragValue(null);
    if (final !== value) {
      onProgress(card, final);
    }
  };

  const handleDelete = (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    // Cards with a linked review card delegate the (combined) confirmation to
    // the board, which deletes both the original and its review card. So do
    // cards the board is going to ask about itself: a browser confirm saying
    // "Delete?" in front of an action that actually KEEPS the card in the
    // previous sprint is how the × came to be read as deletion.
    if (hasLinkedReview || boardAsks) {
      onDelete(card);
      return;
    }
    if (window.confirm(`Delete "${card.title}"?`)) {
      onDelete(card);
    }
  };

  const pickStage = (e: React.MouseEvent<HTMLButtonElement>, stage: StageKey | null) => {
    e.stopPropagation();
    setMenuOpen(false);
    setRecOpen(false);
    onStage(card, stage);
  };

  // The recurrent cycle picker: Recurrent expands a submenu on the right —
  // every sprint (the default), weekly or monthly. Picking a cycle sets the
  // stage and the cycle together.
  const pickRecurrence = (
    e: React.MouseEvent<HTMLButtonElement>,
    cycle: "" | "week" | "month",
  ) => {
    e.stopPropagation();
    setMenuOpen(false);
    setRecOpen(false);
    onStage(card, "recurrent", cycle);
  };

  const pickInProgress = (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    setMenuOpen(false);
    onInProgress(card);
  };

  const pickAssignTeam = (team: string | null) => {
    setAssignOpen(false);
    onSetTeam?.(card, team);
  };

  const pickAssignPerson = (login: string | null) => {
    setAssignOpen(false);
    setPersonInput("");
    onSetAssignee?.(card, login);
  };

  const openDates = (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    if (weekMode) {
      setStartVal(card.week ?? "");
    } else {
      setStartVal(card.startDate ?? "");
      setEndVal(card.day ?? "");
    }
    setDatesOpen((o) => !o);
  };

  // The calendar moves the card's real dates: its start (which also becomes the
  // sprint it belongs to) and its end (due) date — a real relocation.
  const saveDates = () => {
    setDatesOpen(false);
    // A single picked date is a one-day range, not "clear the end".
    onSetDates?.(card, startVal || null, endVal || startVal || null);
  };

  // Defer the card: push its scheduled day (startDate) N days ahead of today —
  // or ahead of its already-deferred slot — leaving its sprint untouched. The
  // boards hide it from today until that day; its past sprint day keeps it.
  const moveStart = (days: number) => {
    setDatesOpen(false);
    onDefer?.(card, days);
  };

  // Plan cards: shift the plan week forward, or set it by picking a date.
  const moveWeek = (days: number) => {
    const base = card.week ?? mondayOf(asOf ?? todayIso());
    setDatesOpen(false);
    onSetWeek?.(card, addDays(base, days));
  };

  const saveWeek = () => {
    setDatesOpen(false);
    onSetWeek?.(card, startVal || null);
  };

  // The card's uid is what an agent needs to act on it (every MCP card tool
  // takes it), so copying it is the one thing worth a click on the card
  // itself — pasting it into a chat beats hunting for it in the API.
  const copyId = (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    const p = navigator.clipboard?.writeText(card.itemId);
    if (!p) {
      return;
    }
    void p.then(() => {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1200);
    });
  };

  // A plan card not yet taken into work hasn't started aging: show 0d. Once it
  // has an assignee it ages normally; and it gets a green "taken" background.
  const taken = Boolean(weekMode) && card.assignees.length > 0;
  // The plan stripe: the stored band, or — for a Project-board slot — the
  // band derived from its end date. A slot needs no stored band to be in
  // the weekly plan, so its row must not pretend otherwise.
  const band = effectiveBand(card);
  // The small second avatar: on a weekly-plan card it is the person the card
  // is ASSIGNED to (who took it into work) — not the review counterpart; on
  // grid cards it stays the counterpart (the reviewer / the implementer).
  const smallAvatars = weekMode
    ? card.assignees
    : (counterpartAssignees ?? []);
  const smallAvatarRole = weekMode
    ? "Assigned to"
    : card.reviewOf
      ? "In implementation"
      : "On review";
  // Age is "days on the board" (its own tooltip): a card scheduled ahead is
  // NOT on the board while it waits, so the parked days must not count — a
  // card created today for next month used to arrive already three weeks old
  // and coloured as rotting. Count from whichever came later, its creation or
  // the day it landed.
  const ageFrom =
    card.startDate && card.createdAt && card.startDate > localDateIso(card.createdAt)
      ? card.startDate
      : card.createdAt;
  const ageDays =
    weekMode && card.assignees.length === 0 ? 0 : daysSince(ageFrom, asOf);

  // The status control: an always-visible icon for review/locked/recurrent,
  // a hover-only flag otherwise. A staged card keeps it after the links icon
  // (the hashtag sits before the stage marker); an unstaged card renders the
  // hover flag BEFORE the links icon, so the always-visible links icon does
  // not jump left when the flag pops in on hover (the actions block is
  // right-anchored, so growth to its left leaves it in place).
  const stageVisible =
    card.stage === "review" ||
    card.stage === "locked" ||
    card.stage === "recurrent";
  const stageControl = (
    <div
      className={`card-stage${card.stage === "review" || card.stage === "locked" || card.stage === "recurrent" ? "" : " card-hoveronly"}`}
      ref={menuRef}
    >
      <button
        type="button"
        className="card-action card-status-btn"
        onClick={(e) => {
          e.stopPropagation();
          setMenuOpen((o) => !o);
        }}
        aria-label="Set status"
        title={
          card.stage === "review"
            ? "On review"
            : card.stage === "recurrent"
              ? recurrenceTitle(card.recurrence, personal)
              : card.stage === "locked"
                ? "Locked"
                : "Status"
        }
        style={card.stage ? { color: STAGES[card.stage].color } : undefined}
      >
        {card.stage === "review" ? (
          <svg
            width="15"
            height="15"
            viewBox="0 0 24 24"
            fill="currentColor"
            aria-hidden="true"
          >
            <rect x="7" y="7" width="3.6" height="10" rx="1.4" />
            <rect x="13.4" y="7" width="3.6" height="10" rx="1.4" />
          </svg>
        ) : card.stage === "recurrent" ? (
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.4"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M21 2v6h-6" />
            <path d="M3 12a9 9 0 0 1 15-6.7L21 8" />
            <path d="M3 22v-6h6" />
            <path d="M21 12a9 9 0 0 1-15 6.7L3 16" />
          </svg>
        ) : card.stage === "locked" ? (
          <svg
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M12 3 2 21h20L12 3z" />
            <line x1="12" y1="10" x2="12" y2="15" />
            <line x1="12" y1="18" x2="12.01" y2="18" />
          </svg>
        ) : (
          "⚑"
        )}
      </button>
      <Dropdown
        open={menuOpen}
        anchorRef={menuRef}
        onClose={() => setMenuOpen(false)}
        className="card-stage-menu"
      >
        <button
          type="button"
          className={`card-stage-item${isInProgress(card) ? " card-stage-item-active" : ""}`}
          onClick={pickInProgress}
        >
          <span
            className="card-stage-dot"
            style={{ background: DEFAULT_BAR_COLOR }}
          />
          In Progress
        </button>
        {STAGE_ORDER.map((stage) =>
          // A review card is auxiliary: it cannot be put on the "review" stage
          // itself, nor made "recurrent" (a repeating task makes no sense for a
          // one-off review).
          (stage === "review" || stage === "recurrent") && card.reviewOf ? null : stage ===
            "recurrent" ? (
            // Recurrent expands a cycle submenu on the right: every sprint
            // (the default, plain click), weekly or monthly.
            <div
              key={stage}
              className="card-stage-rec"
              onMouseEnter={(e) => {
                setRecLeft(
                  e.currentTarget.getBoundingClientRect().right + 130 >
                    window.innerWidth,
                );
                setRecOpen(true);
              }}
              onMouseLeave={() => setRecOpen(false)}
            >
              <button
                type="button"
                className={`card-stage-item${card.stage === "recurrent" ? " card-stage-item-active" : ""}`}
                onClick={(e) => pickRecurrence(e, "")}
              >
                <span
                  className="card-stage-dot"
                  style={{ background: STAGES[stage].color }}
                />
                {STAGES[stage].label}
                <span className="card-stage-sub-arrow" aria-hidden="true">
                  ▸
                </span>
              </button>
              {recOpen && (
                <div
                  className={`card-stage-submenu${recLeft ? " card-stage-submenu-left" : ""}`}
                >
                  {recurrenceCycles
                    .map((cycle) => [cycle, recurrenceLabel(cycle, personal)] as const)
                    .map(([cycle, label]) => (
                    <button
                      key={cycle || "sprint"}
                      type="button"
                      className={`card-stage-item${
                        card.stage === "recurrent" &&
                        (card.recurrence ?? "") === cycle
                          ? " card-stage-item-active"
                          : ""
                      }`}
                      onClick={(e) => pickRecurrence(e, cycle)}
                    >
                      {label}
                    </button>
                  ))}
                </div>
              )}
            </div>
          ) : (
            <button
              key={stage}
              type="button"
              className={`card-stage-item${(stage === "done" ? doneish : card.stage === stage) ? " card-stage-item-active" : ""}`}
              onClick={(e) => pickStage(e, stage)}
            >
              <span
                className="card-stage-dot"
                style={{ background: STAGES[stage].color }}
              />
              {STAGES[stage].label}
            </button>
          ),
        )}
      </Dropdown>
    </div>
  );

  return (
    <div
      className={`card${selected ? " card-selected" : ""}${card.overdue ? " card-overdue" : ""}${(selectedBy?.length ?? 0) > 0 ? " card-peer-selected" : ""}${
        band ? ` card-plan-${band}` : ""
      }${taken ? " card-plan-taken" : ""}${card.reviewOf ? " card-review" : ""}${
        dimAvatar ? " card-dim-avatar" : ""
      }${groupTarget ? " card-group-target" : ""}`}
      onClick={() => onSelect(card)}
      onDoubleClick={() => onOpen(card)}
      title={card.title}
    >
      {(subCount ?? 0) > 0 && onToggleExpand && (
        <button
          type="button"
          className="card-action card-subs-toggle"
          onClick={(e) => {
            e.stopPropagation();
            onToggleExpand(card);
          }}
          onDoubleClick={(e) => e.stopPropagation()}
          aria-expanded={expanded}
          aria-label={expanded ? "Collapse subtasks" : "Expand subtasks"}
          title={`${subCount} subtask(s) — Space toggles`}
        >
          <svg
            width="10"
            height="10"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="3"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            {expanded ? <path d="m6 9 6 6 6-6" /> : <path d="m9 18 6-6-6-6" />}
          </svg>
        </button>
      )}
      <span className="card-title" title={card.title}>
        {card.title}
      </span>
      {domainBadge && (
        <span className="card-domain" title={`Stored in ${domainBadge}`}>
          {domainBadge}
        </span>
      )}


      <span className="card-actions" aria-hidden={false}>
        {!card.itemId.startsWith("tmp-") && (
          <button
            type="button"
            className="card-action card-action-id card-hoveronly"
            onClick={copyId}
            onDoubleClick={(e) => e.stopPropagation()}
            aria-label="Copy card id"
            title={copied ? "Copied" : "Copy card id"}
          >
            {copied ? "✓" : "ID"}
          </button>
        )}
        <button
          type="button"
          className="card-action card-hoveronly card-action-delete"
          onClick={handleDelete}
          aria-label="Delete card"
          title="Delete"
        >
          ×
        </button>
        {onAddSubtask && !card.parent && (
          <button
            type="button"
            className="card-action card-hoveronly card-subs-add"
            onClick={(e) => {
              e.stopPropagation();
              onAddSubtask(card);
            }}
            onDoubleClick={(e) => e.stopPropagation()}
            aria-label="Add a subtask"
            title="Add a subtask"
          >
            +
          </button>
        )}
        {!stageVisible && stageControl}
        {localLinks.length > 0 && (
          <div className="card-links" ref={linksRef}>
            <button
              type="button"
              className="card-action card-links-btn"
              onClick={toggleLinks}
              aria-label="Card links"
              title={hasGitHubRefs ? "Linked issues" : "Links"}
            >
              {hasGitHubRefs ? (
                <svg
                  width="11"
                  height="11"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2.4"
                  strokeLinecap="round"
                  aria-hidden="true"
                >
                  <line x1="4" y1="9" x2="20" y2="9" />
                  <line x1="4" y1="15" x2="20" y2="15" />
                  <line x1="10" y1="3" x2="8" y2="21" />
                  <line x1="16" y1="3" x2="14" y2="21" />
                </svg>
              ) : (
                <LinkGlyph size={11} />
              )}
            </button>
            <Dropdown
              open={linksOpen}
              anchorRef={linksRef}
              onClose={() => setLinksOpen(false)}
              className="card-stage-menu card-links-menu"
            >
              {shownLinks.map((link) => (
                <button
                  key={link.url}
                  type="button"
                  className="card-stage-item card-links-item"
                  title={
                    link.state ? `${link.url} (${link.state})` : link.url
                  }
                  onClick={(e) => {
                    e.stopPropagation();
                    setLinksOpen(false);
                    window.open(link.url, "_blank", "noopener");
                  }}
                >
                  <LinkKindIcon link={link} />
                  <span className="card-links-text">
                    {link.kind === "link"
                      ? link.url
                      : link.title ||
                        `${link.owner}/${link.repo}#${link.number}`}
                  </span>
                </button>
              ))}
            </Dropdown>
          </div>
        )}
        {stageVisible && stageControl}
      </span>

      {selectedBy && selectedBy.length > 0 && (
        <span className="card-presence" aria-hidden="true">
          {selectedBy.map((login, i) => (
            <Avatar
              key={login}
              login={login}
              avatars={avatars}
              names={names}
              className="card-presence-avatar"
              style={{ left: `${-17 - i * 12}px` }}
              title={`Selected by ${displayName(login, names)}`}
            />
          ))}
        </span>
      )}
      {card.reviewOf && (card.reviewRound ?? 0) >= 2 && (
        <span
          className="card-review-round"
          title={`Round ${card.reviewRound} of review`}
        >
          {card.reviewRound}
        </span>
      )}
      {card.createdAt && (
        <div
          className="card-age-wrap"
          ref={ageRef}
          onDoubleClick={(e) => e.stopPropagation()}
        >
          <button
            type="button"
            className="card-age"
            style={{ color: ageColor(ageDays) }}
            title={
              weekMode
                ? "Move the plan week"
                : onSetDates
                  ? "Edit start / end dates"
                  : `On the board ${ageDays} day(s)`
            }
            onClick={canEditDates ? openDates : undefined}
          >
            {ageDays}d
          </button>
          {canEditDates && (
            <Dropdown
              open={datesOpen}
              anchorRef={ageRef}
              onClose={() => setDatesOpen(false)}
              className="card-stage-menu card-dates-menu"
            >
              {weekMode ? (
                <>
                  <div className="card-move-quick">
                    <button
                      type="button"
                      className="card-move-quick-btn"
                      onClick={() => moveWeek(7)}
                    >
                      +1 week
                    </button>
                    <button
                      type="button"
                      className="card-move-quick-btn"
                      onClick={() => moveWeek(14)}
                    >
                      +2 week
                    </button>
                  </div>
                  <RangeCalendar
                    start={startVal || null}
                    end={null}
                    onChange={(s) => setStartVal(s ? mondayOf(s) : "")}
                  />
                  <button type="button" className="card-dates-save" onClick={saveWeek}>
                    Save
                  </button>
                  <button
                    type="button"
                    className="card-dates-cancel"
                    onClick={() => setDatesOpen(false)}
                  >
                    Cancel
                  </button>
                </>
              ) : (
                <>
                  <div className="card-move-quick">
                    <button
                      type="button"
                      className="card-move-quick-btn"
                      onClick={() => moveStart(1)}
                    >
                      +1 day
                    </button>
                    <button
                      type="button"
                      className="card-move-quick-btn"
                      onClick={() => moveStart(7)}
                    >
                      +1 week
                    </button>
                  </div>
                  <RangeCalendar
                    start={startVal || null}
                    end={endVal || null}
                    onChange={(s, e) => {
                      setStartVal(s ?? "");
                      setEndVal(e ?? "");
                    }}
                  />
                  <button type="button" className="card-dates-save" onClick={saveDates}>
                    Save
                  </button>
                  <button
                    type="button"
                    className="card-dates-cancel"
                    onClick={() => setDatesOpen(false)}
                  >
                    Cancel
                  </button>
                </>
              )}
            </Dropdown>
          )}
        </div>
      )}

      {(card.team ||
        canAssign ||
        smallAvatars.length > 0) && (
        <div className="card-assign" ref={assignRef}>
          {card.team ? (
            <button
              type="button"
              className="team-avatar"
              style={{ backgroundColor: teamColor(card.team) }}
              title={
                onSetAssignee
                  ? "Reassign team / person"
                  : onSetTeam
                    ? "Reassign team"
                    : `Team: ${card.team}`
              }
              onClick={
                canAssign
                  ? (e) => {
                      e.stopPropagation();
                      setAssignOpen((o) => !o);
                    }
                  : undefined
              }
            >
              {teamInitial(card.team)}
            </button>
          ) : (
            <button
              type="button"
              className="team-avatar team-avatar-empty"
              title="Assign team / person"
              onClick={(e) => {
                e.stopPropagation();
                setAssignOpen((o) => !o);
              }}
            >
              +
            </button>
          )}
          {smallAvatars.length > 0 && (
            <button
              type="button"
              className="card-counterpart-avatar-btn"
              title={`${smallAvatarRole}: ${smallAvatars
                .map((p) => displayName(p, names))
                .join(", ")}`}
              onClick={
                canAssign
                  ? (e) => {
                      e.stopPropagation();
                      setAssignOpen((o) => !o);
                    }
                  : undefined
              }
            >
              <Avatar
                login={smallAvatars[0]}
                avatars={avatars}
                names={names}
                className="card-counterpart-avatar"
              />
            </button>
          )}
          <Dropdown
            open={assignOpen}
            anchorRef={assignRef}
            onClose={() => setAssignOpen(false)}
            className="card-stage-menu card-assign-menu"
          >
            {card.author && (
              <div className="card-counterpart-head">
                Created by: {card.author}
              </div>
            )}
            {counterpartAssignees && counterpartAssignees.length > 0 && (
              <div className="card-counterpart-head">
                {card.reviewOf ? "In implementation" : "On review"}:{" "}
                {counterpartAssignees
                  .map((p) => p)
                  .join(", ")}
              </div>
            )}
            {/* What this card is part of — the menu is where a person asks
                "whose is this?", and the answer starts with where it came
                from. */}
            {(card.process || card.project) && (
              <div className="card-assign-origin">
                {card.process && (
                  <span>
                    <span className="card-assign-origin-kind">process</span>
                    {card.process}
                  </span>
                )}
                {card.project && (
                  <span>
                    <span className="card-assign-origin-kind">project</span>
                    {card.project}
                    {card.epic ? ` · ${card.epic}` : ""}
                  </span>
                )}
              </div>
            )}
            <div className="card-assign-cols">
              {onSetTeam && (
                <div className="card-assign-col">
                  <div className="card-assign-head">Team</div>
                  {(teams ?? []).map((t) => (
                    <button
                      key={`t-${t}`}
                      type="button"
                      className={`card-stage-item${card.team === t ? " card-stage-item-active" : ""}`}
                      onClick={() => pickAssignTeam(t)}
                    >
                      <span className="team-dot" style={{ background: teamColor(t) }} />
                      {t}
                    </button>
                  ))}
                  <button
                    type="button"
                    className="card-stage-item card-stage-clear"
                    onClick={() => pickAssignTeam(null)}
                  >
                    No team
                  </button>
                </div>
              )}
              {onSetAssignee && (
                <div className="card-assign-col">
                  <div className="card-assign-head">Person</div>
                  {(people ?? []).map((p) => (
                    <button
                      key={`p-${p}`}
                      type="button"
                      className={`card-stage-item${card.assignees.includes(p) ? " card-stage-item-active" : ""}`}
                      onClick={() => pickAssignPerson(p)}
                    >
                      <Avatar login={p} avatars={avatars} names={names} />
                      {displayName(p, names)}
                    </button>
                  ))}
                  <button
                    type="button"
                    className="card-stage-item card-stage-clear"
                    onClick={() => pickAssignPerson(null)}
                  >
                    Unassigned
                  </button>
                  <input
                    type="text"
                    className="add-card-input card-assign-input"
                    placeholder="login…"
                    value={personInput}
                    onClick={(e) => e.stopPropagation()}
                    onChange={(e) => setPersonInput(e.target.value)}
                    onKeyDown={(e) => {
                      e.stopPropagation();
                      if (e.key === "Enter" && personInput.trim()) {
                        pickAssignPerson(personInput.trim());
                      } else if (e.key === "Escape") {
                        setAssignOpen(false);
                      }
                    }}
                  />
                </div>
              )}
              {card.stage === "review" && onSetReviewAssignee && (
                <div className="card-assign-col">
                  <div className="card-assign-head">Reviewer</div>
                  {(reviewers ?? people ?? []).map((p) => (
                    <button
                      key={`r-${p}`}
                      type="button"
                      className={`card-stage-item${(counterpartAssignees ?? []).includes(p) ? " card-stage-item-active" : ""}`}
                      onClick={() => {
                        onSetReviewAssignee(card, p);
                        setAssignOpen(false);
                      }}
                    >
                      <Avatar login={p} avatars={avatars} names={names} />
                      {displayName(p, names)}
                    </button>
                  ))}
                  <button
                    type="button"
                    className="card-stage-item card-stage-clear"
                    onClick={() => {
                      onSetReviewAssignee(card, null);
                      setAssignOpen(false);
                    }}
                  >
                    Remove reviewer
                  </button>
                  <input
                    type="text"
                    className="add-card-input card-assign-input"
                    placeholder="reviewer login…"
                    value={reviewerInput}
                    onClick={(e) => e.stopPropagation()}
                    onChange={(e) => setReviewerInput(e.target.value)}
                    onKeyDown={(e) => {
                      e.stopPropagation();
                      if (e.key === "Enter" && reviewerInput.trim()) {
                        onSetReviewAssignee(card, reviewerInput.trim());
                        setReviewerInput("");
                        setAssignOpen(false);
                      } else if (e.key === "Escape") {
                        setAssignOpen(false);
                      }
                    }}
                  />
                </div>
              )}
            </div>
          </Dropdown>
        </div>
      )}

      <div
        className="card-bar"
        ref={barRef}
        title={
          (subCount ?? 0) > 0
            ? `${shown}% — derived from subtasks`
            : `${shown}%`
        }
      >
        {Array.from({ length: SEGMENTS }, (_, i) => (
          <span
            key={i}
            className={`card-seg${i < filled ? " card-seg-filled" : ""}`}
            style={i < filled ? { backgroundColor: fill } : undefined}
          />
        ))}
        <div
          className={`card-bar-handle${dragValue !== null ? " card-bar-handle-dragging" : ""}`}
          // Sit the handle on the last filled 10% boundary, not the raw value, so
          // an off-grid value (e.g. 95) doesn't leave it floating past the fill.
          style={{ left: `${filled * 10}%`, borderColor: fill }}
          role="slider"
          aria-label="Progress"
          aria-valuenow={shown}
          aria-valuemin={0}
          aria-valuemax={100}
          onClick={(e) => e.stopPropagation()}
          onPointerDown={onHandleDown}
          onPointerMove={onHandleMove}
          onPointerUp={onHandleUp}
          onPointerCancel={onHandleUp}
        />
      </div>
    </div>
  );
}

/** LinkGlyph is the chain icon for plain links. */
function LinkGlyph({ size = 13 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
      <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
    </svg>
  );
}

/** linkStateColor picks the GitHub-ish colour for an issue/PR state. */
function linkStateColor(link: CardLink): string {
  switch (link.state) {
    case "closed":
      return link.kind === "pull" ? "#cf222e" : "#8250df";
    case "merged":
      return "#8250df";
    case "draft":
      return "#6e7781";
    default:
      return "#1a7f37";
  }
}

/** LinkKindIcon renders the GitHub-style glyph for an issue / pull request /
 * plain link, with the shape AND colour following the resolved state (open,
 * closed, merged, draft) the way github.com draws them. */
function LinkKindIcon({ link }: { link: CardLink }) {
  const glyph = (path: JSX.Element) => (
    <svg
      width="13"
      height="13"
      viewBox="0 0 16 16"
      fill={linkStateColor(link)}
      aria-hidden="true"
    >
      {path}
    </svg>
  );
  if (link.kind === "issue") {
    if (link.state === "closed") {
      // Issue closed: a check in a circle.
      return glyph(
        <>
          <path d="M11.28 6.78a.75.75 0 0 0-1.06-1.06L7.25 8.69 5.78 7.22a.75.75 0 0 0-1.06 1.06l2 2a.75.75 0 0 0 1.06 0l3.5-3.5Z" />
          <path d="M16 8A8 8 0 1 1 0 8a8 8 0 0 1 16 0Zm-1.5 0a6.5 6.5 0 1 0-13 0 6.5 6.5 0 0 0 13 0Z" />
        </>,
      );
    }
    // Issue open: the dotted circle.
    return glyph(
      <>
        <path d="M8 9.5a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3Z" />
        <path d="M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0ZM1.5 8a6.5 6.5 0 1 0 13 0 6.5 6.5 0 0 0-13 0Z" />
      </>,
    );
  }
  if (link.kind === "pull") {
    if (link.state === "merged") {
      // Merged: the git-merge glyph.
      return glyph(
        <path d="M5.45 5.154A4.25 4.25 0 0 0 9.25 7.5h1.378a2.251 2.251 0 1 1 0 1.5H9.25A5.734 5.734 0 0 1 5 7.123v3.505a2.25 2.25 0 1 1-1.5 0V5.372a2.25 2.25 0 1 1 1.95-.218ZM4.25 13.5a.75.75 0 1 0 0-1.5.75.75 0 0 0 0 1.5Zm8.5-4.5a.75.75 0 1 0 0-1.5.75.75 0 0 0 0 1.5ZM5 3.25a.75.75 0 1 0 0 .005V3.25Z" />,
      );
    }
    if (link.state === "closed") {
      // Closed unmerged: the crossed-out pull-request glyph.
      return glyph(
        <path d="M3.25 1A2.25 2.25 0 0 1 4 5.372v5.256a2.251 2.251 0 1 1-1.5 0V5.372A2.251 2.251 0 0 1 3.25 1Zm9.5 5.5a.75.75 0 0 1 .75.75v3.378a2.251 2.251 0 1 1-1.5 0V7.25a.75.75 0 0 1 .75-.75Zm-2.03-5.273a.75.75 0 0 1 1.06 0l.97.97.97-.97a.748.748 0 0 1 1.265.332.75.75 0 0 1-.205.729l-.97.97.97.97a.751.751 0 0 1-.018 1.042.751.751 0 0 1-1.042.018l-.97-.97-.97.97a.749.749 0 0 1-1.275-.326.749.749 0 0 1 .215-.734l.97-.97-.97-.97a.75.75 0 0 1 0-1.06ZM2.5 3.25a.75.75 0 1 0 1.5 0 .75.75 0 0 0-1.5 0ZM3.25 12a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm9.5 0a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Z" />,
      );
    }
    // Open (or draft, greyed): the pull-request glyph.
    return glyph(
      <path d="M1.5 3.25a2.25 2.25 0 1 1 3 2.122v5.256a2.251 2.251 0 1 1-1.5 0V5.372A2.25 2.25 0 0 1 1.5 3.25Zm5.677-.177L9.573.677A.25.25 0 0 1 10 .854V2.5h1A2.5 2.5 0 0 1 13.5 5v5.628a2.251 2.251 0 1 1-1.5 0V5a1 1 0 0 0-1-1h-1v1.646a.25.25 0 0 1-.427.177L7.177 3.427a.25.25 0 0 1 0-.354ZM3.75 2.5a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm0 9.5a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm8.25.75a.75.75 0 1 0 1.5 0 .75.75 0 0 0-1.5 0Z" />,
    );
  }
  return <LinkGlyph />;
}
