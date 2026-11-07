import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { clientId, fetchConfig, fetchHealth, type AppConfig } from "./api/client";
import { ApiError, apiProvider } from "./providers/api/apiProvider";
import { guardSignedOut } from "./session";
import { mergeCardLists } from "./cardmerge";
import { resourceToCard, sprintStateFrom, type WatchFrame } from "./api/resources";
import type { Board, Card as CardModel, SprintState } from "./providers/types";
import { MeBoard } from "./components/MeBoard";
import { TeamBoard } from "./components/TeamBoard";
import { BacklogBoard } from "./components/BacklogBoard";
import { ProjectBoard } from "./components/ProjectBoard";
import { RoadmapBoard } from "./components/RoadmapBoard";
import { cardIsGone, followFrame, followPatch } from "./detail";
import { applyOrder, type OrderArg } from "./cardorder";
import { presenceAfter, type PresenceMap } from "./presence";
import { PROJECT_CARDS_KEY, storedProjectCards } from "./roadmap";
import { ROADMAP_FILTERS_KEY, sprintTeamOf, storedRoadmapFilters, type RoadmapFilters } from "./ownership";
import { TeamsModal } from "./components/TeamsModal";
import { CustomersModal } from "./components/CustomersModal";
import { readProjectFilter, writeProjectFilter } from "./projectFilter";
import {
  boardMetadata,
  membersFrom,
  showingDay,
  standingOn,
} from "./providers/api/apiProvider";
import { CardDetail } from "./components/CardDetail";
import { Logo } from "./components/Logo";
import { DayNav } from "./components/DayNav";
import { avatarsFrom, namesFrom } from "./users";
import { forgeCopy } from "./forge";
import { unpushedNotice, type HealthStatus } from "./health";
import { migrateBoardScopedKeys } from "./storage";
import { pruneTeamFilter, settlePendingTeams, teamRoster } from "./teams";
import { forgetMade, justMade } from "./justmade";
import {
  queryString,
  snapshotDay,
  storedView,
  viewPath,
  viewQueries,
  watchQueries,
  type ViewMode,
} from "./viewquery";
import { frozenProvider } from "./providers/frozen";
import { todayIso, setBoardTimezone } from "./date";
import { doneAtAfter } from "./stages";
import { mergeNotes } from "./notes";
import { Tombstones, withTombstones } from "./tombstones";
import { buildLink, cardLink, cardUnreadable, linkChange, parseLink, saveReturnTo, startDay, takeReturnTo, type LinkState } from "./deeplink";
import { openWatches } from "./watch";
import { nameConflict } from "./names";
import { AppearanceMenu } from "./components/AppearanceMenu";
import { applyAppearance, persistAppearance, readAppearance, type Appearance } from "./theme";

// waitingAfterMs is how long a request may take before the board says it is
// waiting. Below it nothing is shown: the answer arrives before the eye
// would settle on the indicator anyway.
const waitingAfterMs = 300;

// Reaching into a day that is OVER is answered with the board the reader was
// after, not with a sentence: the day leaves the record for today, and the
// team whose card was touched is offered its carry-over — a lead reaching
// into a past day is usually a lead whose sprint has not been closed yet.
// The write itself is still refused (it was aimed at a picture), and nothing
// is said, because a banner explaining a thing that already happened is
// noise.

const LS_VIEW = "aeman.view";
const LS_TEAM_FILTER = "aeman.teamFilter";
// The Team board's and the Backlog's Customer filter (O11): one selection the
// two share, as they share the team one with the Me board, which has no
// Customer filter. The Roadmap keeps its own (ROADMAP_FILTERS_KEY).
const LS_CUSTOMER_FILTER = "aeman.customerFilter";
const LS_VIEW_AS = "aeman.viewAs";

function readView(): ViewMode {
  return storedView(localStorage.getItem(LS_VIEW));
}

// The board's address at load (deeplink.ts): a deep link wins over the
// remembered tab. The address a sign-in began on is taken off on every load,
// whatever happens to it, and kept aside only for an address with nothing in
// it — it is given back once the config says the visitor is signed in, so a
// sign-in abandoned long ago, or a sign-out back to the front page, never
// resurfaces it.
function initialAddress(): { link: LinkState; kept: string | null } {
  let kept: string | null = null;
  try {
    kept = takeReturnTo(sessionStorage, Date.now());
  } catch {
    // no session storage: nothing was kept
  }
  return { link: parseLink(window.location.search), kept: window.location.search ? null : kept };
}

function readStringList(key: string): string[] | null {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) {
      return null;
    }
    const parsed = JSON.parse(raw) as unknown;
    return Array.isArray(parsed) ? parsed.filter((x): x is string => typeof x === "string") : null;
  } catch {
    return null;
  }
}

// The team filter is the one piece of team state this browser keeps — a
// preference. The server serves one board, so it lives under a plain key; the
// picker era scoped it per board, and the first load under this build moves
// the last board's value over (once — see storage.ts). The teams themselves
// are the server's (board.teams) and are never remembered here: a remembered
// roster once leaked from one board onto the next served from the same origin.
function readFilter(): string[] | null {
  migrateBoardScopedKeys(localStorage, [LS_TEAM_FILTER]);
  const arr = readStringList(LS_TEAM_FILTER);
  return arr && arr.length ? arr : null;
}

const errMessage = (err: unknown) =>
  err instanceof Error ? err.message : String(err);

// listingMoment is the moment a set of listings reflects: a snapshot's `asOf`
// when the server answered with a past day's board, null when it answered
// live. One answer decides for the view — the listings of a view are all of
// the same moment because they carry the same day.
function listingMoment(lists: { asOf?: string }[]): string | null {
  for (const l of lists) {
    if (l.asOf) {
      return l.asOf;
    }
  }
  return null;
}

export function App() {
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [tokenWarningDismissed, setTokenWarningDismissed] = useState(false);

  const [{ link, kept }] = useState(initialAddress);
  const [view, setView] = useState<ViewMode>(() => link.view ?? readView());
  // The Project filter's selection on the Project tab.
  const [projectFilter, setProjectFilterState] = useState<string[] | null>(readProjectFilter);
  const setProjectFilter = (keys: string[] | null) => {
    setProjectFilterState(keys);
    writeProjectFilter(keys);
  };
  // The project manager: one dialog, reached from the Project tab's chip
  // row. Its writes return as soon as the server's cache has them; the Board
  // watch frame repaints — nothing reloads.
  const [managingProjects, setManagingProjects] = useState(false);
  // The team and customer rosters' dialogs, opened from the bottom of the
  // Team and Customer filters on any board that offers them.
  const [managingTeams, setManagingTeams] = useState(false);
  const [managingCustomers, setManagingCustomers] = useState(false);
  // `domain` is the repository the project is declared in (multi-domain
  // boards offer the choice; otherwise the server picks the primary).
  const addProject = (name: string, domain?: string) => {
    if (!board || !name.trim()) {
      return;
    }
    // Names are one namespace across the board's repositories; the server
    // would refuse this too, but the form should say so first.
    const conflict = nameConflict("project", board.projects, name);
    if (conflict) {
      setError(conflict);
      return;
    }
    setProjectFilter([name.trim()]);
    void provider
      .addProject(name.trim(), domain)
      .catch((err: unknown) => setError(errMessage(err)));
  };
  const deleteProject = (name: string) => {
    if (!board || !window.confirm(`Delete the project “${name}”?`)) {
      return;
    }
    if (projectFilter?.includes(name)) {
      setProjectFilter(null);
    }
    void provider.deleteProject(name).catch((err: unknown) => setError(errMessage(err)));
  };
  const renameProject = (from: string, to: string) => {
    if (!board) {
      return;
    }
    const conflict = nameConflict("project", board.projects, to, from);
    if (conflict) {
      setError(conflict);
      return;
    }
    if (projectFilter?.includes(from)) {
      setProjectFilter(projectFilter.map((p) => (p === from ? to : p)));
    }
    void provider.renameProject(from, to).catch((err: unknown) => setError(errMessage(err)));
  };
  const reorderProjects = (ordered: string[]) => {
    if (!board) {
      return;
    }
    // Shown in the new order at once; the frame confirms it.
    setBoard((cur) => (cur ? { ...cur, projects: ordered } : cur));
    void provider.reorderProjects(ordered).catch((err: unknown) => setError(errMessage(err)));
  };

  // Appearance (theme mode + colour palette). Applied to <html> so the CSS
  // theme/palette overrides repaint the app; persisted in localStorage like the
  // app's other UI prefs. useLayoutEffect (not useEffect) so the attributes land
  // before the browser paints — otherwise a returning user on a non-default
  // appearance gets one frame of the default look on every load. When the mode
  // is "system" we also follow live OS light/dark changes.
  const [appearance, setAppearance] = useState<Appearance>(() => readAppearance(localStorage));
  useLayoutEffect(() => {
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const apply = () => applyAppearance(document.documentElement, appearance, mq.matches);
    apply();
    persistAppearance(localStorage, appearance);
    if (appearance.mode !== "system") {
      return;
    }
    mq.addEventListener("change", apply);
    return () => mq.removeEventListener("change", apply);
  }, [appearance]);

  const [board, setBoard] = useState<Board | null>(null);
  // The viewed day, shared by both boards and driving the lazy view fetch and
  // the scoped watch. Lifted out of the boards so the App owns what to load.
  const [selectedDate, setSelectedDate] = useState<string>(() => link.day ?? todayIso());
  // "View as" impersonation on the Me board — lifted here because the Me fetch
  // must carry the impersonated user explicitly (the server otherwise resolves
  // the caller's own login).
  // Persisted: an impersonating lead refreshing the page stays on the same
  // person's board (a card just created for them would otherwise "vanish"
  // when the view silently snapped back to the lead's own).
  const [viewAs, setViewAs] = useState<string | null>(
    () => localStorage.getItem(LS_VIEW_AS) || null,
  );
  const setViewAsPersisted = useCallback((login: string | null) => {
    setViewAs(login);
    if (login) {
      localStorage.setItem(LS_VIEW_AS, login);
    } else {
      localStorage.removeItem(LS_VIEW_AS);
    }
  }, []);
  // Avatars come with the roster (GET /board members); a login outside it —
  // an assignee who is not a member — is drawn as initials.
  const avatars = useMemo(() => avatarsFrom(board?.members ?? []), [board?.members]);
  // Display names come with the roster too (a GitLab board has them, a GitHub
  // one does not); they decorate labels only — the login stays the identifier.
  const names = useMemo(() => namesFrom(board?.members ?? []), [board?.members]);
  // The forge (GitHub / GitLab) spells the sign-in and token copy; before the
  // config answers, and on an older server, that is GitHub.
  const forge = useMemo(() => forgeCopy(config), [config]);
  // Count of in-flight data loads (initial load + per-view card fetches, plus
  // the slow actions the boards wrap in trackLoad); any of them showing is
  // what the waiting mark beside the view switch reports.
  const [pendingLoads, setPendingLoads] = useState(0);
  const loading = pendingLoads > 0;
  // waiting is `loading` that has LASTED: a request answered in a blink shows
  // nothing at all. The old bar across the top appeared for every fetch,
  // however short, and a flash at the edge of the screen reads as something
  // being wrong rather than as work in progress.
  const [waiting, setWaiting] = useState(false);
  // Moving to another view ends the moment the × acts in silence: what the
  // reader made in the view they left is no longer in front of them, and a
  // card they are meeting again is a decision (justmade.ts).
  useEffect(() => {
    forgetMade();
  }, [view]);

  useEffect(() => {
    if (!loading) {
      setWaiting(false);
      return;
    }
    const t = window.setTimeout(() => setWaiting(true), waitingAfterMs);
    return () => window.clearTimeout(t);
  }, [loading]);
  const beginLoad = useCallback(() => setPendingLoads((n) => n + 1), []);
  const endLoad = useCallback(() => setPendingLoads((n) => n - 1), []);
  // Boards wrap their slow server calls (carry over etc.) with this so the
  // waiting mark covers the operation itself, not just the refetch after it.
  const trackLoad = useCallback(
    <T,>(p: Promise<T>): Promise<T> => {
      beginLoad();
      return p.finally(endLoad);
    },
    [beginLoad, endLoad],
  );
  // Server-side writes not yet committed (the write queue), fed by Queue
  // watch frames; shown as a small counter that trends to zero.
  // Frames arrive on every enqueue/drain step — a rapid burst of edits would
  // re-render the whole app dozens of times — so the badge updates at most a
  // few times a second (trailing debounce keeps the final value accurate).
  const [pendingSync, setPendingSync] = useState(0);
  const pendingSyncNext = useRef(0);
  const pendingSyncTimer = useRef<number | null>(null);
  const queuePendingSync = useCallback((n: number) => {
    pendingSyncNext.current = n;
    if (pendingSyncTimer.current !== null) {
      return;
    }
    pendingSyncTimer.current = window.setTimeout(() => {
      pendingSyncTimer.current = null;
      setPendingSync(pendingSyncNext.current);
    }, 300);
  }, []);
  const [error, setErrorState] = useState<string | null>(null);
  // While a sign-out is being confirmed (see onSignedOut) the messages the
  // failing calls produce — every one of them "not signed in" — are held
  // back: the sign-in gate is the answer, not a banner. The last one held is
  // shown after all if the session turns out to be fine (a transient 401).
  const holdingErrors = useRef(false);
  const heldError = useRef<string | null>(null);
  const setError = useCallback((message: string | null) => {
    if (message !== null && holdingErrors.current) {
      heldError.current = message;
      return;
    }
    setErrorState(message);
  }, []);
  // Live selections of other users (login -> card uid), fed by Presence
  // watch frames; purely ephemeral shared-cursor state.
  const [presence, setPresenceMap] = useState<PresenceMap>({});
  // A board's selection, shared live (#290): Me, Team and Backlog all say
  // which card their reader has selected, and draw everyone else's.
  const sharePresence = (card: string | null) => {
    if (config?.login) {
      void provider.setPresence(config.login, card).catch(() => {});
    }
  };
  const [detailCard, setDetailCard] = useState<CardModel | null>(null);
  // The cards the pane was opened THROUGH: a crumb followed, a child row, the
  // task a subtask rides — each opens its card in the same drawer, and the
  // way back is this stack. A board opening a card starts afresh.
  const [detailStack, setDetailStack] = useState<CardModel[]>([]);
  // The card the address names, still being fetched (deeplink.ts): while it
  // is, the address is left alone. Anything the person does with the drawer
  // or the tabs supersedes it.
  const pendingCard = useRef<string | null>(link.card);
  // The next change of the open card is not the person's (the server took
  // the card away, a linked card arrived): it rewrites the address rather
  // than adding a step to the history. Set only right before such a change,
  // and consumed by the next run of the address effect.
  const replaceNext = useRef(false);
  const openCard = (c: CardModel) => {
    pendingCard.current = null;
    setDetailStack([]);
    setDetailCard(c);
  };
  const openWithin = (c: CardModel) => {
    pendingCard.current = null;
    setDetailStack((s) => (detailCard ? [...s, detailCard] : s));
    setDetailCard(c);
  };
  // The drawer's Back is the browser's: opening a card within pushed a step,
  // and going back pops it (the popstate handler puts the previous card back
  // from the stack), so the two Backs never disagree.
  const backInDetail = () => {
    window.history.back();
  };
  const closeDetail = () => {
    pendingCard.current = null;
    setDetailStack([]);
    setDetailCard(null);
  };
  // A tab chosen supersedes a linked card still being fetched.
  const chooseView = (v: ViewMode) => {
    pendingCard.current = null;
    setView(v);
  };
  // A card the Roadmap is asked to REVEAL — a breadcrumb followed from a
  // detail pane: the tab switches and the row is unfolded and marked.
  const [roadmapFocus, setRoadmapFocus] = useState<string | null>(null);
  const revealOnRoadmap = (uid: string) => {
    closeDetail();
    setRoadmapFocus(uid);
    chooseView("roadmap");
  };

  // Teams are the server's (board.teams). The only team state here is the
  // just-added ones whose create is in flight — shown at once, and gone from
  // this list as soon as the board declares them (see teams.ts). Nothing of it
  // is persisted.
  const [pendingTeams, setPendingTeams] = useState<string[]>([]);
  // Team filter: null = all, else the selected groups ("" = no-team), several
  // being OR (the Team dropdown's checkboxes). Persisted, and pruned to the
  // teams the board has once it is loaded.
  const [teamFilter, setTeamFilter] = useState<string[] | null>(readFilter);
  // Customer filter of the day boards: [] = none, else customer ids (OR).
  const [customerFilter, setCustomerFilterState] = useState<string[]>(() => readStringList(LS_CUSTOMER_FILTER) ?? []);
  const setCustomerFilter = useCallback((next: string[]) => {
    setCustomerFilterState(next);
    try {
      if (next.length === 0) {
        localStorage.removeItem(LS_CUSTOMER_FILTER);
      } else {
        localStorage.setItem(LS_CUSTOMER_FILTER, JSON.stringify(next));
      }
    } catch {
      // a preference, not state
    }
  }, []);
  // The Roadmap's "Project cards" toggle is the listing's selector, not a
  // filter of what was sent: it rides the roadmap's queries and watch
  // (viewquery, projectCards), so turning it refetches and re-subscribes.
  // Remembered per viewer.
  const [projectCards, setProjectCardsState] = useState<boolean>(() => {
    try {
      return storedProjectCards(localStorage.getItem(PROJECT_CARDS_KEY));
    } catch {
      return storedProjectCards(null);
    }
  });
  // The Roadmap's Team, Customer and Owner filters (ownership.md, O7, O8):
  // the listing's selectors, like the toggle above; remembered per viewer.
  const [roadmapFilters, setRoadmapFiltersState] = useState<RoadmapFilters>(() => {
    try {
      return storedRoadmapFilters(localStorage.getItem(ROADMAP_FILTERS_KEY));
    } catch {
      return storedRoadmapFilters(null);
    }
  });
  const setRoadmapFilters = useCallback((next: RoadmapFilters) => {
    setRoadmapFiltersState(next);
    try {
      localStorage.setItem(ROADMAP_FILTERS_KEY, JSON.stringify(next));
    } catch {
      // a preference, not state
    }
  }, []);
  const setProjectCards = useCallback((next: boolean) => {
    setProjectCardsState(next);
    try {
      localStorage.setItem(PROJECT_CARDS_KEY, next ? "1" : "0");
    } catch {
      // a preference, not state
    }
  }, []);

  // The sprint pointers as they stand TODAY. A past day is served as a record
  // — its cards, its roster and its pointers are that day's — so the board in
  // hand cannot say where the current sprint is while a record is on screen,
  // and the jump that exists to bring somebody back was reading the very day
  // it was meant to leave (which is why it sat disabled all the way back).
  const [liveSprints, setLiveSprints] = useState<Record<string, SprintState>>({});

  // A tab can stay open across deploys (and, on a dev box, across dozens of
  // rebuilds) — it keeps running the bundle it loaded, which reads as "the fix
  // did not work". The server fingerprints the bundle it serves, so a mismatch
  // is checked whenever the tab is looked at again, and offered as a reload.
  const [staleBuild, setStaleBuild] = useState(false);
  useEffect(() => {
    const mine = config?.build;
    if (!mine) {
      return;
    }
    const check = () => {
      if (document.hidden) {
        return;
      }
      void fetchConfig()
        .then((cfg) => setStaleBuild(!!cfg.build && cfg.build !== mine))
        .catch(() => undefined);
    };
    window.addEventListener("focus", check);
    document.addEventListener("visibilitychange", check);
    const timer = window.setInterval(check, 60_000);
    return () => {
      window.removeEventListener("focus", check);
      document.removeEventListener("visibilitychange", check);
      window.clearInterval(timer);
    };
  }, [config?.build]);

  // The sync state: every change is committed at once, but the push can fall
  // behind (a remote outage, a rejected push the server keeps rebasing).
  // /healthz says "degraded" past the server's threshold; polled on the build
  // check's cadence and shown as a non-blocking banner while it lasts.
  const [health, setHealth] = useState<HealthStatus | null>(null);
  const configured = config !== null;
  useEffect(() => {
    if (!configured) {
      return;
    }
    const check = () => {
      if (document.hidden) {
        return;
      }
      void fetchHealth()
        .then(setHealth)
        .catch(() => undefined);
    };
    check();
    window.addEventListener("focus", check);
    document.addEventListener("visibilitychange", check);
    const timer = window.setInterval(check, 60_000);
    return () => {
      window.removeEventListener("focus", check);
      document.removeEventListener("visibilitychange", check);
      window.clearInterval(timer);
    };
  }, [configured]);
  const syncNotice = unpushedNotice(health);

  // Bootstrap: fetch the session config; the board loads once it is known.
  useEffect(() => {
    let cancelled = false;
    fetchConfig()
      .then((cfg) => {
        if (cancelled) {
          return;
        }
        setConfig(cfg);
        // The board's "today" runs in the server's zone for everyone.
        setBoardTimezone(cfg.tz);
        // Back from signing in: the address it began on, given back once
        // (kept only when the board was opened with an empty address; the
        // address effect may have written the remembered tab into it since).
        let start = link;
        if (cfg.authenticated && kept) {
          window.history.replaceState(null, "", kept);
          start = parseLink(window.location.search);
          if (start.view) {
            setView(start.view);
          }
          pendingCard.current = start.card;
        }
        // The linked day outlives the start; today only when none was named.
        setSelectedDate(startDay(start, todayIso()));
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(errMessage(err));
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    localStorage.setItem(LS_VIEW, view);
  }, [view]);

  // The tab is named after the board — the one thing this server serves.
  useEffect(() => {
    document.title = board?.title ? `${board.title} — aeman` : "aeman";
  }, [board?.title]);

  // A tab left open past midnight keeps a stale "today". When it regains focus,
  // catch the viewed day up to the real today — unless the user navigated to a
  // specific other day. (Formerly TeamBoard owned this.)
  const lastTodayRef = useRef(todayIso());
  useEffect(() => {
    const sync = () => {
      const now = todayIso();
      if (now === lastTodayRef.current) {
        return;
      }
      setSelectedDate((d) => (d === lastTodayRef.current ? now : d));
      lastTodayRef.current = now;
    };
    document.addEventListener("visibilitychange", sync);
    window.addEventListener("focus", sync);
    return () => {
      document.removeEventListener("visibilitychange", sync);
      window.removeEventListener("focus", sync);
    };
  }, []);

  // A session can end under an open tab: the server restarted with memory-only
  // sessions, the session TTL passed, the forge refused a refresh. From then on
  // every request is a 401. In OAuth mode that is not an error to read but a
  // sign-in to offer: the config is re-read and, once it says "not
  // authenticated", the gate below renders in place of the board. In
  // local-proxy mode a 401 is an ordinary error — there is nothing to sign in
  // to. One re-check at a time, however many calls fail meanwhile; every
  // provider call on the board goes through the guard, so whichever saw the
  // 401 first starts it.
  const configRef = useRef(config);
  configRef.current = config;
  const recheck = useRef<Promise<void> | null>(null);
  const onSignedOut = useCallback(() => {
    if (configRef.current?.mode !== "oauth" || recheck.current) {
      return;
    }
    holdingErrors.current = true;
    recheck.current = fetchConfig()
      .then((cfg) => {
        if (!cfg.authenticated) {
          heldError.current = null;
          setConfig(cfg);
        }
      })
      .catch(() => undefined)
      .finally(() => {
        // Still signed in (a transient 401), or no answer at all: the error
        // was real — show what was held.
        recheck.current = null;
        holdingErrors.current = false;
        if (heldError.current !== null) {
          setErrorState(heldError.current);
          heldError.current = null;
        }
      });
  }, []);
  // Looking BACK on the Me or Team board shows that day as it was — the
  // server reads the board out of its own history (see snapshotDay). A day
  // that is over is not a place to work: the provider is frozen, so a
  // handler that did not think about the date cannot write today's board
  // from a view of the past, and the drag sensors are off for the same
  // reason.
  const [asOf, setAsOf] = useState<string | null>(null);
  // A day is frozen when the SERVER answered with a past day's board — it
  // owns that judgement, because a day inside the running sprint is still
  // live however far back the calendar has moved (the sprint lays itself out
  // on its own day and the team works it from there). Asking is the client's
  // part: snapshotDay decides which days are worth asking about.
  const snapshot = asOf !== null && snapshotDay(view, selectedDate);

  // The way back to the sprint being worked, beside the day it moves. One
  // destination — the CURRENT sprint — with the arrow pointing the way:
  // "Current sprint »" when it is ahead of the day on screen, "« Current
  // sprint" when it is behind. Null while there is nowhere to go (no sprint
  // at all, or the board already standing on its day), and the button is not
  // drawn at all then: an arrow that points nowhere is worse than no arrow.
  //
  // It reads the LIVE pointers, never the board's: on a past day the board is
  // that day's record, whose current sprint is the day itself, so the jump
  // that exists to leave a record was asking the record where to go.
  const sprintJump = useMemo<{ target: string; dir: "back" | "fwd" } | null>(() => {
    const states =
      teamFilter?.length === 1
        ? [liveSprints[teamFilter[0]]].filter(Boolean)
        : Object.values(liveSprints);
    let cur: string | null = null;
    for (const s of states) {
      if (s?.current && (cur === null || s.current > cur)) {
        cur = s.current;
      }
    }
    if (cur === null || cur === selectedDate) {
      return null;
    }
    return { target: cur, dir: selectedDate < cur ? "fwd" : "back" };
  }, [liveSprints, teamFilter, selectedDate]);
  useEffect(() => {
    if (!snapshotDay(view, selectedDate)) {
      setAsOf(null);
    }
  }, [view, selectedDate]);
  // The server judges a write by the day it was made from, so it has to know
  // which day is on screen — including while the answer is still in flight.
  useEffect(() => {
    showingDay(snapshotDay(view, selectedDate) ? selectedDate : "");
  }, [view, selectedDate]);
  // Which cards on screen are RECORDS: a card says so itself (`asOf`), set by
  // the server for the teams whose sprint has moved past the day being looked
  // at. One screen can hold both — a team still inside that sprint is working
  // it, and its cards stay live.
  const records = useMemo(() => {
    const ids = new Set<string>();
    for (const c of board?.cards ?? []) {
      if (c.asOf) {
        ids.add(c.itemId);
      }
    }
    return ids;
  }, [board?.cards]);
  const liveBoard = useRef(board);
  liveBoard.current = board;
  // Leaving the record: the day jumps to today, and the team of the card that
  // was reached for is offered its carry-over (the Team board runs the same
  // flow its button does, confirm and all, and says nothing when the sprint
  // is already today). Returns the message the refused call carries — none.
  const [carryRequest, setCarryRequest] = useState<{
    team: string | null;
    at: number;
  }>();
  const leaveTheRecord = useCallback((uid: string): string => {
    setSelectedDate(todayIso());
    const card = liveBoard.current?.cards.find((c) => c.itemId === uid);
    if (card) {
      // The carry-over of the team the card stands under (O13): an
      // inheriting task's is its plan item's team's, not the no-team group's.
      setCarryRequest({ team: sprintTeamOf(card, liveBoard.current?.cards ?? []), at: Date.now() });
    }
    return "";
  }, []);

  // The watch handler and the provider are built once; they read the current
  // records through a ref rather than closing over them.
  const recordsRef = useRef(records);
  recordsRef.current = records;
  const snapshotRef = useRef(snapshot);
  snapshotRef.current = snapshot;
  // The cards this tab deleted, so a frame sent mid-delete cannot draw one
  // again (tombstones.ts).
  const tombstones = useRef(new Tombstones());
  const provider = useMemo(
    () =>
      frozenProvider(
        withTombstones(guardSignedOut(apiProvider, onSignedOut), tombstones.current),
        (uid) => recordsRef.current.has(uid),
        // A write that names no card (a create, a carry-over) is refused
        // whenever the board being read IS a record — the same question the
        // server asks of such a write, and the same one the add boxes are
        // hidden by. Three doors, one rule.
        () => snapshotRef.current,
        (uid) => leaveTheRecord(uid),
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [onSignedOut],
  );

  // The roster: the board's teams in the server-side order (the sprint-state
  // cards' positions — shared by everyone, on every device), then the ones
  // just added here that the server has not declared yet. Every team a card
  // names is declared (the server writes the team file on every path that
  // puts a team on a card), so nothing is read off the cards.
  const boardTeams = board?.teams;
  const roster = useMemo(
    () => teamRoster(boardTeams ?? [], pendingTeams),
    [boardTeams, pendingTeams],
  );

  // A just-added team is the board's once the Board frame carries it; the
  // pending list only bridges the moment in between.
  useEffect(() => {
    if (boardTeams) {
      setPendingTeams((cur) => settlePendingTeams(cur, boardTeams));
    }
  }, [boardTeams]);

  // A saved filter can outlive its teams (a team renamed away, another board
  // served from this origin before): entries no team backs would silently
  // blank the board, so prune them — and an emptied filter means "all" again.
  // Only once the board is loaded: before that there is nothing to prune
  // against.
  const boardLoaded = board !== null;

  // ---- the board's address (deeplink.ts) ----
  // The view, the day of a day board and the open card live in the address.
  // Opening, switching or closing a card is a step Back undoes (pushState);
  // a tab or a day only rewrites the address (replaceState). A card the
  // address names is fetched by uid — it may stand on another board, day or
  // team than the one on screen — and while it is being fetched the address
  // is left alone, or the first render would take the card off it.
  const detailRef = useRef(detailCard);
  detailRef.current = detailCard;
  const stackRef = useRef(detailStack);
  stackRef.current = detailStack;
  const placeRef = useRef({ view, selectedDate });
  placeRef.current = { view, selectedDate };
  const openByLink = useCallback(
    (uid: string) => {
      pendingCard.current = uid;
      void provider
        .getCard(uid)
        .then((c) => {
          if (pendingCard.current !== uid) {
            return;
          }
          pendingCard.current = null;
          replaceNext.current = true;
          setDetailStack([]);
          setDetailCard(c);
        })
        .catch((err: unknown) => {
          if (pendingCard.current !== uid) {
            return;
          }
          pendingCard.current = null;
          if (!cardUnreadable(err)) {
            // The network or the server: an error to show; the address
            // keeps the card, so a reload tries it again.
            setError(errMessage(err));
            return;
          }
          // Not this visitor's to read, or gone: say so, and take it off the
          // address — written from the board on screen — without a step in
          // the history.
          const { view: v, selectedDate: d } = placeRef.current;
          window.history.replaceState(null, "", window.location.pathname + buildLink({ view: v, card: null, day: d }, todayIso()));
          setError("That card is not on this board, or it is not one you can read.");
        });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [provider],
  );
  useEffect(() => {
    if (boardLoaded && pendingCard.current) {
      openByLink(pendingCard.current);
    }
    // Once, when the board first arrives.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [boardLoaded]);
  useEffect(() => {
    if (pendingCard.current) {
      return;
    }
    const uid = detailCard && !detailCard.itemId.startsWith("tmp-") ? detailCard.itemId : null;
    const next = buildLink({ view, card: uid, day: selectedDate }, todayIso());
    const change = linkChange(window.location.search, next);
    const url = window.location.pathname + next;
    if (change === "push" && !replaceNext.current) {
      window.history.pushState(null, "", url);
    } else if (change !== "none") {
      window.history.replaceState(null, "", url);
    }
    replaceNext.current = false;
  }, [view, detailCard, selectedDate]);
  // Back and Forward: the address says what to show.
  useEffect(() => {
    // The address already says what the board now shows, so none of this
    // writes it again (the address effect finds nothing to change).
    const onPop = () => {
      const s = parseLink(window.location.search);
      if (s.view) {
        setView(s.view);
      }
      setSelectedDate(s.day ?? todayIso());
      const stack = stackRef.current;
      if (!s.card) {
        pendingCard.current = null;
        setDetailStack([]);
        setDetailCard(null);
      } else if (stack.length > 0 && stack[stack.length - 1].itemId === s.card) {
        // Back to the card this one was opened from: it is on the stack.
        pendingCard.current = null;
        setDetailCard(stack[stack.length - 1]);
        setDetailStack(stack.slice(0, -1));
      } else if (s.card !== detailRef.current?.itemId) {
        openByLink(s.card);
      }
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, [openByLink]);
  useEffect(() => {
    if (!boardLoaded) {
      return;
    }
    setTeamFilter((cur) => pruneTeamFilter(cur, roster));
  }, [boardLoaded, roster]);
  // A customer deleted since is no filter: an id nobody backs would blank
  // the day boards.
  const customerIds = (board?.customers ?? []).map((c) => c.id).join("\u0000");
  useEffect(() => {
    if (!boardLoaded) {
      return;
    }
    const known = new Set(customerIds.split("\u0000"));
    if (customerFilter.some((id) => !known.has(id))) {
      setCustomerFilter(customerFilter.filter((id) => known.has(id)));
    }
  }, [boardLoaded, customerIds, customerFilter, setCustomerFilter]);

  // What the active board loads and watches: Me is the reader's own (the
  // server fills in "who am I" unless view-as impersonates someone), Team
  // names the teams it shows (the filter, or the whole roster) and loads the
  // day grid PLUS the day being looked at. activeKey / watchKey are stable
  // serialisations used to re-fetch and re-subscribe only when the selection
  // actually changes.
  const activeQueries = useMemo(
    // No filter means ALL: the roster's teams plus the no-team group, so an
    // unfiltered Team board misses nothing (the client filter mirrors this —
    // teamFilter === null passes every card).
    () =>
      viewQueries(
        view,
        selectedDate,
        teamFilter ?? [...new Set([...roster, ""])],
        viewAs ?? undefined,
        undefined,
        { projectCards, filters: roadmapFilters, customers: customerFilter },
      ),
    [view, selectedDate, teamFilter, roster, viewAs, projectCards, roadmapFilters, customerFilter],
  );
  const activeKey = activeQueries.map(queryString).join("|");
  // Which BOARD the reader is standing on, for the gestures made on it: the ×,
  // the owner picker, a create. It is the listing's own
  // selector minus what only a listing needs, so the server judges a press
  // against exactly the board the person is looking at.
  const standingKey = useMemo(() => {
    // `reviews` stays: it is part of what the board DREW, and the gate the
    // server applies to a gesture is the listing itself — asked without it,
    // the two would answer differently about a review card the board is
    // showing. Only `snapshot` goes, and only because a write from a past
    // day is refused before any of this (recordWriteGuard).
    const { snapshot: _snapshot, ...scope } = activeQueries[0] ?? {};
    return queryString(scope);
  }, [activeQueries]);
  useEffect(() => {
    standingOn(standingKey);
  }, [standingKey]);
  // The board (roster + sprint pointers) is fetched for the SAME moment as
  // the cards: the view rules compare a card's sprint against the pointers,
  // so a past day's cards under today's pointers are nearly all dropped —
  // which looks exactly like the snapshot not working.
  const boardQuery = useMemo<Record<string, string>>(() => {
    if (!snapshotDay(view, selectedDate)) {
      return {};
    }
    // The SAME selector the cards are asked with, minus what only a listing
    // needs: where the past begins depends on whose sprints are in view, and
    // the three reads of one board (cards, sprints, roster) must agree on it
    // or the day's cards land under another moment's pointers.
    const { reviews: _reviews, ...rest } = activeQueries[0] ?? {};
    return { ...rest, snapshot: "1" };
  }, [view, selectedDate, activeQueries]);
  const boardKey = queryString(boardQuery);
  const watchKeys = useMemo(
    () =>
      watchQueries(
        view,
        selectedDate,
        teamFilter ?? [...new Set([...roster, ""])],
        viewAs ?? undefined,
        { projectCards, filters: roadmapFilters, customers: customerFilter },
      ).map(queryString),
    [view, selectedDate, teamFilter, roster, viewAs, projectCards, roadmapFilters, customerFilter],
  );
  // One string for the watch effect's dep: the sockets are rebuilt only when
  // the selections actually change.
  const watchKey = watchKeys.join("|");

  // Reorder the whole roster (from the manage dialog). Teams the server knows
  // (sprint pointer) get the order pushed to the board itself — their hidden
  // sprint-state cards are moved, so the order is shared by the whole team.
  // Teams still pending keep their relative order until the server has them.
  const reorderTeams = useCallback(
    (ordered: string[]) => {
      setPendingTeams((cur) => ordered.filter((t) => cur.includes(t)));
      const cur = board;
      if (!cur) {
        return;
      }
      const server = ordered.filter((t) => cur.teams.includes(t));
      if (server.length < 2) {
        return;
      }
      setBoard((b) => (b ? { ...b, teams: server } : b));
      void provider.reorderTeams(server).catch((err: unknown) => {
        // Keep the optimistic order on screen; the next reload converges it
        // back to the server's truth if the write really failed.
        setError(errMessage(err));
      });
    },
    [board, provider],
  );

  // Add a team: shown at once from the pending list, and declared on the
  // server as a sprint pointer with no dates yet — `domain` picks the
  // repository its roster entry is written to (the primary unless chosen).
  // The Board frame then carries it as one of the board's own teams and the
  // pending entry settles away.
  const addTeam = useCallback(
    (team: string, domain?: string) => {
      const t = team.trim();
      if (!t || !board || board.teams.includes(t)) {
        return;
      }
      setPendingTeams((cur) => (cur.includes(t) ? cur : [...cur, t]));
      void provider.setSprintState(t, null, null, domain).catch((err: unknown) => {
        // Refused (a taken name, no write access): the chip must not outlive
        // the refusal — it was never the board's.
        setPendingTeams((cur) => cur.filter((x) => x !== t));
        setError(errMessage(err));
      });
    },
    [board, provider],
  );

  const removeTeam = useCallback(
    (team: string) => {
      const dropLocal = () => {
        setPendingTeams((cur) => cur.filter((t) => t !== team));
        // Drop the removed team from the filter (clearing it if it becomes empty).
        setTeamFilter((cur) => {
          if (cur === null) {
            return cur;
          }
          const next = cur.filter((k) => k !== team);
          return next.length ? next : null;
        });
      };
      const cur = board;
      if (!cur || !cur.teams.includes(team)) {
        // Still pending: the server has nothing to delete yet.
        dropLocal();
        return;
      }
      // A board-backed team is deleted on the server (its hidden sprint-state
      // card goes away); the server refuses while cards still use the team,
      // and that message is the user's answer.
      void provider
        .deleteTeam(team)
        .then(() => {
          dropLocal();
          setBoard((b) =>
            b
              ? {
                  ...b,
                  teams: b.teams.filter((t) => t !== team),
                  sprintStates: Object.fromEntries(
                    Object.entries(b.sprintStates).filter(([k]) => k !== team),
                  ),
                }
              : b,
          );
        })
        .catch((err: unknown) => setError(errMessage(err)));
    },
    [board, provider],
  );

  // Persist the filter whenever it changes (null means "all", we store
  // nothing).
  useEffect(() => {
    if (teamFilter === null) {
      localStorage.removeItem(LS_TEAM_FILTER);
    } else {
      localStorage.setItem(LS_TEAM_FILTER, JSON.stringify(teamFilter));
    }
  }, [teamFilter]);

  // Declared before doLoad, which reads them: it runs long after the render,
  // so the closure worked either way, but a temporal-dead-zone trap is not
  // worth leaving lying around.
  const loadedBoardKey = useRef("");
  const activeQueriesRef = useRef(activeQueries);
  const boardQueryRef = useRef(boardQuery);
  const doLoad = useCallback(async () => {
    beginLoad();
    setError(null);
    try {
      // Identity + sprints AND the active view's cards together, swapped in
      // as one board: a reload() must never leave the board empty while the
      // cards are still in flight (loadBoard itself carries no cards).
      const [loaded, lists] = await Promise.all([
        provider.loadBoard(boardQueryRef.current),
        Promise.all(activeQueriesRef.current.map((q) => provider.listCards(q))),
      ]);
      setAsOf(listingMoment(lists));
      loadedBoardKey.current = queryString(boardQueryRef.current);
      // An EMPTY board query is the live board: remember its pointers, since
      // a record's are its own day's (see liveSprints).
      if (Object.keys(boardQueryRef.current).length === 0) {
        setLiveSprints(loaded.sprintStates);
      }
      setBoard((cur) => ({
        ...loaded,
        cards: mergeCardLists(lists.map((l) => l.cards), cur?.cards, justMade),
      }));
    } catch (err: unknown) {
      // A day the history no longer reaches (410) — or storage that keeps
      // none (501) — has no cards to show, and leaving the last day's on
      // screen would put them under the new date. The listing effect draws
      // the same line; a reload landing straight on such a day must too.
      if (err instanceof ApiError && (err.status === 410 || err.status === 501)) {
        setAsOf(null);
        setBoard((cur) => (cur ? { ...cur, cards: [] } : cur));
      }
      setError(errMessage(err));
    } finally {
      endLoad();
    }
  }, [provider, beginLoad, endLoad]);
  boardQueryRef.current = boardQuery;

  // Load the active view's cards whenever the selection (view/day/teams) changes
  // or the board is (re)loaded. loadBoard brings only identity + sprints; the
  // cards for one view arrive here, so the UI holds just what it shows.
  activeQueriesRef.current = activeQueries;
  useEffect(() => {
    if (!boardLoaded) {
      return;
    }
    let cancelled = false;
    beginLoad();
    const wantBoard = loadedBoardKey.current !== boardKey;
    Promise.all([
      Promise.all(activeQueriesRef.current.map((q) => provider.listCards(q))),
      wantBoard ? provider.loadBoard(boardQuery) : Promise.resolve(null),
    ])
      .then(([lists, loaded]) => {
        if (cancelled) {
          return;
        }
        if (loaded) {
          loadedBoardKey.current = boardKey;
          if (Object.keys(boardQuery).length === 0) {
            setLiveSprints(loaded.sprintStates);
          }
        }
        setAsOf(listingMoment(lists));
        // The listing is the row view; the notes, events and bodies already
        // fetched are kept, so switching views costs one request, not one
        // per card all over again. A board fetched alongside (the day moved
        // into or out of a snapshot) replaces the roster and the pointers,
        // never the cards — those are the listing's.
        setBoard((cur) =>
          cur
            ? {
                ...cur,
                ...(loaded ? { ...loaded, cards: cur.cards } : {}),
                cards: mergeCardLists(lists.map((l) => l.cards), cur.cards, justMade),
              }
            : cur,
        );
      })
      .catch((err: unknown) => {
        if (cancelled) {
          return;
        }
        // A day the history no longer reaches (410) — or storage that keeps
        // none (501) — has no cards to show. Leaving the previous day's on
        // screen would put them under the new date, which is the one thing a
        // record must never do.
        if (err instanceof ApiError && (err.status === 410 || err.status === 501)) {
          setBoard((cur) => (cur ? { ...cur, cards: [] } : cur));
        }
        setError(errMessage(err));
      })
      .finally(endLoad);
    return () => {
      cancelled = true;
    };
  }, [boardLoaded, activeKey, boardKey, boardQuery, provider, beginLoad, endLoad]);

  // The board loads once the session is known (and, in OAuth mode, signed in).
  // A visitor the server turns away gets the load error as the placeholder.
  const loadAttempted = useRef(false);
  useEffect(() => {
    if (config?.authenticated && !loadAttempted.current) {
      loadAttempted.current = true;
      void doLoad();
    }
  }, [config, doLoad]);

  const reload = useCallback(() => {
    if (boardLoaded) {
      void doLoad();
    }
  }, [boardLoaded, doLoad]);

  // patch is a plain partial, or an updater computing one from the card's
  // CURRENT state — rapid successive changes (notes typed Enter-Enter-Enter)
  // must not base themselves on a stale render's copy.
  const patchCard = useCallback(
    (
      itemId: string,
      patch: Partial<CardModel> | ((c: CardModel) => Partial<CardModel>),
    ) => {
      setBoard((cur) => {
        if (!cur) {
          return cur;
        }
        // An empty patch (an updater deciding nothing changed) keeps the
        // exact same state object, so React bails out of the re-render.
        let changed = false;
        const cards = cur.cards.map((c) => {
          if (c.itemId !== itemId) {
            return c;
          }
          const p = typeof patch === "function" ? patch(c) : patch;
          if (!p || Object.keys(p).length === 0) {
            return c;
          }
          changed = true;
          const next = { ...c, ...p };
          // The day a card reached done goes with it, exactly as the server
          // writes it (P6). A day board draws finished work by that day and no
          // other, so an optimistic copy without it is a card that vanishes
          // between the click and the answer.
          if (!("doneAt" in p)) {
            next.doneAt = doneAtAfter(c, p, todayIso());
          }
          return next;
        });
        return changed ? { ...cur, cards } : cur;
      });
      // The drawer's card follows the same patch when no board lists it — a
      // card opened by uid — or it would show the old one until a reload.
      setDetailCard((d) => followPatch(d, itemId, patch));
      setDetailStack((st) => (st.some((c) => c.itemId === itemId) ? st.map((c) => followPatch(c, itemId, patch) ?? c) : st));
    },
    [],
  );

  // addCard upserts by item id: the watch stream may deliver a created card
  // before (or after) the creator's own response lands, so both paths must
  // converge on a single copy instead of appending twice. Locally-loaded notes
  // survive the upsert — Card resources never carry notes (they live in the
  // notes subresource).
  const addCard = useCallback((card: CardModel) => {
    setBoard((cur) => {
      if (!cur) {
        return cur;
      }
      const exists = cur.cards.some((c) => c.itemId === card.itemId);
      return {
        ...cur,
        cards: exists
          ? cur.cards.map((c) =>
              c.itemId === card.itemId
                ? { ...card, notes: card.notes ?? c.notes }
                : c,
            )
          : [...cur.cards, card],
      };
    });
  }, []);

  // Swap an optimistic card for its server twin IN PLACE, so a burst of
  // creates keeps its visual order (append-on-ack would reshuffle them).
  const replaceCard = useCallback((itemId: string, card: CardModel) => {
    setBoard((cur) => {
      if (!cur) {
        return cur;
      }
      if (!cur.cards.some((c) => c.itemId === itemId)) {
        const exists = cur.cards.some((c) => c.itemId === card.itemId);
        return exists
          ? cur
          : { ...cur, cards: [...cur.cards, card] };
      }
      return {
        ...cur,
        cards: cur.cards
          .filter((c) => c.itemId !== card.itemId || c.itemId === itemId)
          .map((c) => {
            if (c.itemId === itemId) {
              return card;
            }
            // Optimistic subtasks created under the tmp id follow the swap,
            // or they would orphan (and their rows flicker away) until their
            // own acks land.
            if (c.parent === itemId) {
              return { ...c, parent: card.itemId };
            }
            return c;
          }),
      };
    });
  }, []);

  const removeCard = useCallback((itemId: string) => {
    setBoard((cur) =>
      cur ? { ...cur, cards: cur.cards.filter((c) => c.itemId !== itemId) } : cur,
    );
  }, []);

  // Reorder board.cards to match orderedIds. Cards whose ids are not listed keep
  // their relative order and are appended after the explicitly ordered ones.
  const reorderCards = useCallback((order: OrderArg) => {
    setBoard((cur) => (cur ? { ...cur, cards: applyOrder(cur.cards, order) } : cur));
  }, []);

  // Live updates: the server pushes resource events over an unscoped WebSocket
  // watch (Kubernetes style). We LIST via loadBoard, then mirror the ADDED /
  // MODIFIED / DELETED frames: Card frames upsert/remove by uid, Sprint frames
  // update the team's pointer, Ordering frames re-sort the local cards. On a
  // socket drop we re-LIST after reconnecting to reconcile anything missed,
  // and at once when the server says the watch fell behind (G85, watch.ts).
  // Refs keep the socket from being rebuilt on every board change.
  const reloadRef = useRef(reload);
  reloadRef.current = reload;
  const boardRef = useRef(board);
  boardRef.current = board;
  // The day feed (MeBoard) subscribes here to hear about Card watch frames —
  // a foreign note or move must land in the feed without a reload. Own
  // mutations mostly never arrive (the watch is keyed by ?client=, and the
  // server skips this tab's echoes) — except a DELETED for a card gone from
  // the board, which the author is told too (G87); relaying it drops an
  // entry that is already gone, which changes nothing.
  const cardFrameListeners = useRef(new Set<(uid: string, deleted: boolean) => void>());
  const subscribeCardFrames = useCallback(
    (fn: (uid: string, deleted: boolean) => void) => {
      cardFrameListeners.current.add(fn);
      return () => {
        cardFrameListeners.current.delete(fn);
      };
    },
    [],
  );
  // Not while signed out: the server refuses the socket, and rebuilding it
  // every few seconds behind the sign-in gate helps nobody. The tear-down
  // below runs the moment the re-check flips the config.
  const authenticated = config?.authenticated === true;
  useEffect(() => {
    if (!boardLoaded || !authenticated) {
      return;
    }
    const applyFrame = (frame: WatchFrame) => {
      // A past day on screen is a picture of a day that ended. Live frames
      // are about TODAY's board — applying them here would edit the picture
      // card by card until it showed neither day. The board's own structure
      // (projects, columns, the sync counter) is not of any day and still
      // arrives; returning to today re-lists everything anyway.
      if (snapshotRef.current) {
        // The records on screen are of a day that ended; today's traffic is
        // not about them, and applying it would edit the picture card by
        // card. The board's own structure still arrives, and the live half
        // of the same screen keeps its stream (a Card frame is checked
        // against the record set below).
        if (frame.kind === "Sprint" || frame.kind === "Ordering") {
          return;
        }
        if (frame.kind === "Card") {
          const uid = frame.object.metadata.uid;
          if (uid && recordsRef.current.has(uid)) {
            return;
          }
        }
      }
      // Sync marks a finished server-side reload; the diff already arrived as
      // ordinary events, so there is nothing left to do here.
      if (frame.kind === "Sync") {
        return;
      }
      // The board's STRUCTURE changed — a project, a column, a deadline. The
      // frame carries the board itself, so it is applied the way a Card frame
      // is: no round trip, and our own roster writes need no reload either.
      if (frame.kind === "Board") {
        const obj = frame.object;
        setBoard((cur) => (cur ? { ...cur, ...boardMetadata(obj) } : cur));
        return;
      }
      // What the board's PEOPLE are holding: cards carried, points carried,
      // points a week. Derived from cards across every team, so this cannot
      // be computed from the view's own cards — a size set on a card this tab
      // is not showing still moves the number over its owner. It arrives
      // apart from the roster because it moves on every write and the roster
      // hardly ever does.
      if (frame.kind === "Load") {
        setBoard((cur) =>
          cur ? { ...cur, members: membersFrom(frame.object.members) } : cur,
        );
        return;
      }
      // The write queue's depth: changes applied everywhere but not yet
      // committed.
      if (frame.kind === "Queue") {
        queuePendingSync(frame.object.pending);
        return;
      }
      // A write the store finally rejected: the board has been rolled back to
      // the server's reloaded state; surface what was lost.
      if (frame.kind === "SyncError") {
        setError(frame.object.message || "a change could not be saved");
        return;
      }
      if (frame.kind === "Card") {
        // A frame that would bring back a card this tab just deleted — sent
        // between the delete's writes — is turned away; its DELETED is not.
        if (tombstones.current.blocks(frame.object.metadata.uid ?? "", frame.type)) {
          return;
        }
        const card = resourceToCard(frame.object);
        const notifyCardFrame = (deleted: boolean) => {
          for (const fn of cardFrameListeners.current) {
            fn(card.itemId, deleted);
          }
        };
        // The drawer's card hears its frames even when no board lists it, and
        // closes only for a card deleted outright: one that left this board's
        // selection is still the card it shows.
        setDetailCard((d) => {
          const next = followFrame(d, card, cardIsGone(frame));
          // Taken away by the server, not closed by the person: the address
          // is rewritten, no step added to the history.
          if (d && !next) {
            replaceNext.current = true;
          }
          return next;
        });
        if (frame.type === "DELETED") {
          removeCard(card.itemId);
          notifyCardFrame(true);
          return;
        }
        // Note/event changes arrive as plain card changes, and the resource
        // carries neither: refetch the log when this card's was already loaded.
        const existing = boardRef.current?.cards.find(
          (c) => c.itemId === card.itemId,
        );
        addCard(card);
        notifyCardFrame(false);
        if (existing?.notes !== undefined) {
          void provider
            .listLog(card.itemId)
            .then(({ notes, events }) =>
              // Merge, don't replace: the response may predate a local
              // optimistic note added while it was in flight.
              patchCard(card.itemId, (c) => ({
                notes: mergeNotes(notes, c.notes),
                events,
              })),
            )
            .catch(() => {});
        }
        return;
      }
      if (frame.kind === "Sprint") {
        const sprint = frame.object;
        setBoard((cur) =>
          cur
            ? {
                ...cur,
                sprintStates: {
                  ...cur.sprintStates,
                  [sprint.metadata.team ?? ""]: sprintStateFrom(sprint),
                },
              }
            : cur,
        );
        return;
      }
      if (frame.kind === "Ordering") {
        reorderCards(frame.object.spec.uids);
        return;
      }
      if (frame.kind === "Presence") {
        const { login, card, client } = frame.object;
        if (!login) {
          return;
        }
        // Keyed by the tab it is from, as the server keys it (presence.ts).
        setPresenceMap((cur) => presenceAfter(cur, { login, card, client }));
      }
    };
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    // Scope the watch to the active view: Me watches its day selection, Team
    // watches every card of the teams it shows, whatever day it lands on. A
    // card entering the selection arrives as ADDED, one leaving as DELETED.
    // ?client= keeps our own mutations from echoing back. Re-subscribes when
    // watchKey changes (a dep below). The board is a path segment here too
    // (viewquery.viewPath), so a socket and the listing beside it address the
    // same board. One socket per selector, kept in step by watch.ts, which
    // also answers a watch the server ended for falling behind (G85).
    return openWatches({
      urls: watchKey.split("|").map((key) => {
        const path = viewPath(key, "watch");
        const sep = path.includes("?") ? "&" : "?";
        return `${proto}//${window.location.host}/api/v1${path}${sep}client=${clientId}`;
      }),
      onFrame: applyFrame,
      relist: () => reloadRef.current(),
      // A fresh connection replays the presence and queue snapshots; drop
      // stale marks (the queue may have drained while we were away).
      onConnect: () => {
        setPresenceMap({});
        queuePendingSync(0);
      },
    });
  }, [
    boardLoaded,
    authenticated,
    watchKey,
    provider,
    addCard,
    removeCard,
    patchCard,
    reorderCards,
    queuePendingSync,
  ]);

  const onError = useCallback((message: string) => setError(message), [setError]);

  // Rename a team everywhere: the roster, the filter, and every card using it.
  const renameTeam = useCallback(
    (from: string, to: string) => {
      const t = to.trim();
      if (!t || t === from) {
        return;
      }
      // The local side of a rename: the pending list, the filter, and the
      // loaded board (its cards and the team's sprint pointer) read the new
      // name.
      const relabel = () => {
        setPendingTeams((cur) => [...new Set(cur.map((x) => (x === from ? t : x)))]);
        setTeamFilter((cur) =>
          cur === null ? cur : cur.map((k) => (k === from ? t : k)),
        );
        setBoard((cur) => {
          if (!cur) {
            return cur;
          }
          const { [from]: pointer, ...others } = cur.sprintStates;
          return {
            ...cur,
            teams: cur.teams.map((x) => (x === from ? t : x)),
            sprintStates: pointer === undefined ? cur.sprintStates : { ...others, [t]: pointer },
            cards: cur.cards.map((c) => (c.team === from ? { ...c, team: t } : c)),
          };
        });
      };
      // A team the board declares is renamed by the server — its file and
      // every card that names it, in one action; a name another team has
      // is refused there. A chip the board does not know yet is only a
      // local label.
      if (boardRef.current?.teams.includes(from)) {
        void provider
          .renameTeam(from, t)
          .then(relabel)
          .catch((err: unknown) => setError(errMessage(err)));
        return;
      }
      relabel();
    },
    [provider],
  );

  const showTokenWarning =
    config !== null && !config.tokenAvailable && !tokenWarningDismissed;

  // OAuth mode: gate the whole UI behind the forge's sign-in.
  if (config && config.mode === "oauth" && !config.authenticated) {
    return (
      <div className="app">
        <header className="app-header">
          <div className="brand">
            <Logo className="brand-logo" />
            <span className="version">{config.version}</span>
          </div>
        </header>
        <div className="signin">
          <h2>{forge.signInTitle}</h2>
          <p>{forge.signInLead}</p>
          <a
            className="btn btn-primary signin-btn"
            href={config.authUrl ?? "/auth/login"}
            // A deep link followed before signing in is given back after it.
            onClick={() => {
              try {
                saveReturnTo(sessionStorage, window.location.pathname + window.location.search, Date.now());
              } catch {
                // no session storage: the front page it is
              }
            }}
          >
            {forge.signInButton}
          </a>
        </div>
      </div>
    );
  }

  return (
    <div className="app">
      <header className="app-header">
        <div className="brand">
          <Logo className="brand-logo" />
          {config && <span className="version">{config.version}</span>}
        </div>
        {board && (
          <span className="board-title" title={board.url || undefined}>
            {board.title}
          </span>
        )}
        <div className="account">
          <AppearanceMenu
            login={config?.login ?? null}
            appearance={appearance}
            onChange={setAppearance}
            logoutUrl={
              config?.mode === "oauth" && config.authenticated
                ? (config.logoutUrl ?? "/auth/logout")
                : null
            }
          />
        </div>
      </header>

      {staleBuild && (
        <div className="banner banner-warning" role="alert">
          <span>A newer build of aeman is being served — this tab is running the previous one.</span>
          <button
            type="button"
            className="btn btn-primary banner-action"
            onClick={() => window.location.reload()}
          >
            Reload
          </button>
        </div>
      )}

      {syncNotice && (
        <div className="banner banner-warning" role="status">
          <span>{syncNotice}</span>
        </div>
      )}

      {showTokenWarning && (
        <div className="banner banner-warning" role="alert">
          {/* forge.noTokenHint, with the commands set in <code>. */}
          <span>
            No {forge.label} token — run <code>aeman login</code>, or{" "}
            <code>{forge.cli} auth login</code>, in the terminal where aeman runs.
          </span>
          <button
            type="button"
            className="banner-close"
            onClick={() => setTokenWarningDismissed(true)}
            aria-label="Dismiss"
          >
            ×
          </button>
        </div>
      )}

      {error && (
        <div className="banner banner-error" role="alert">
          <span>{error}</span>
          <button
            type="button"
            className="banner-close"
            onClick={() => setError(null)}
            aria-label="Dismiss"
          >
            ×
          </button>
        </div>
      )}

      <div className="toolbar">
        {/* The day belongs to the app, not to a board: it is the same value
            whichever day board is open, so the control that moves it stands
            with the view tabs rather than being drawn again inside each. The
            boards that are not of a day (Backlog, Roadmap, Project) have no
            day to show. */}
        {(view === "me" || view === "team") && (
          <DayNav
            selectedDate={selectedDate}
            onSelectDate={setSelectedDate}
            sprintJump={view === "team" ? sprintJump : null}
          />
        )}

        {pendingSync === 0 && waiting && (
          <span className="load-badge" title="Loading…" role="status" aria-label="Loading">
            <svg width="12" height="12" viewBox="0 0 24 24" aria-hidden="true">
              <circle
                cx="12"
                cy="12"
                r="9"
                fill="none"
                stroke="currentColor"
                strokeWidth="3"
                strokeLinecap="round"
                strokeDasharray="14 42"
              />
            </svg>
          </span>
        )}
        {pendingSync > 0 && (
          <span
            className="sync-badge"
            title={`${pendingSync} change(s) applied but not yet committed`}
          >
            <svg
              width="11"
              height="11"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.5"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <path d="M21 2v6h-6" />
              <path d="M3 12a9 9 0 0 1 15-6.7L21 8" />
              <path d="M3 22v-6h6" />
              <path d="M21 12a9 9 0 0 1-15 6.7L3 16" />
            </svg>
            {pendingSync}
          </span>
        )}
        <div className="segmented" role="tablist" aria-label="View">
          <button
            type="button"
            role="tab"
            aria-selected={view === "me"}
            className={`segment${view === "me" ? " segment-active" : ""}`}
            onClick={() => chooseView("me")}
          >
            Me
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={view === "team"}
            className={`segment${view === "team" ? " segment-active" : ""}`}
            onClick={() => chooseView("team")}
          >
            Team
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={view === "backlog"}
            className={`segment${view === "backlog" ? " segment-active" : ""}`}
            onClick={() => chooseView("backlog")}
          >
            Backlog
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={view === "roadmap"}
            className={`segment${view === "roadmap" ? " segment-active" : ""}`}
            onClick={() => chooseView("roadmap")}
          >
            Roadmap
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={view === "project"}
            className={`segment${view === "project" ? " segment-active" : ""}`}
            onClick={() => chooseView("project")}
          >
            Project
          </button>
        </div>
      </div>

      <main className="content">
        {!board && !loading && (
          <p className="placeholder">
            {error ? "The board could not be loaded." : "Loading…"}
          </p>
        )}
        {board && view === "me" && (
          <MeBoard
            board={board}
            selectedDate={selectedDate}
            asOf={asOf ?? undefined}
            viewAs={viewAs}
            onViewAs={setViewAsPersisted}
            provider={provider}
            me={config?.login ?? ""}
            avatars={avatars}
            names={names}
            connectHint={forge.connectHint}
            subscribeCardFrames={subscribeCardFrames}
            teams={roster}
            teamFilter={teamFilter}
            onSetFilter={setTeamFilter}
            patchCard={patchCard}
            addCard={addCard}
            replaceCard={replaceCard}
            removeCard={removeCard}
            reorderCards={reorderCards}
            reload={reload}
            onError={onError}
            onOpen={openCard}
            onPresence={sharePresence}
            presence={presence}
          />
        )}
        {board && view === "project" && (
          <ProjectBoard
            board={board}
            provider={provider}
            filter={projectFilter}
            onSetFilter={setProjectFilter}
            onManageProjects={() => setManagingProjects(true)}
            patchCard={patchCard}
            addCard={addCard}
            replaceCard={replaceCard}
            removeCard={removeCard}
            reload={reload}
            onError={onError}
            onOpen={openCard}
          />
        )}
        {board && view === "roadmap" && (
          <RoadmapBoard
            board={board}
            provider={provider}
            roster={roster}
            avatars={avatars}
            names={names}
            patchCard={patchCard}
            addCard={addCard}
            replaceCard={replaceCard}
            removeCard={removeCard}
            reorderCards={reorderCards}
            reload={reload}
            onError={onError}
            onOpen={openCard}
            focusId={roadmapFocus}
            onFocused={() => setRoadmapFocus(null)}
            showProjects={projectCards}
            onShowProjects={setProjectCards}
            filters={roadmapFilters}
            onFilters={setRoadmapFilters}
            onManageTeams={() => setManagingTeams(true)}
            onManageCustomers={() => setManagingCustomers(true)}
          />
        )}
        {board && view === "backlog" && (
          <BacklogBoard
            board={board}
            provider={provider}
            roster={roster}
            teamFilter={teamFilter}
            onSetFilter={setTeamFilter}
            customerFilter={customerFilter}
            onSetCustomerFilter={setCustomerFilter}
            onManageTeams={() => setManagingTeams(true)}
            onManageCustomers={() => setManagingCustomers(true)}
            avatars={avatars}
            names={names}
            patchCard={patchCard}
            addCard={addCard}
            removeCard={removeCard}
            reorderCards={reorderCards}
            reload={reload}
            onOpen={openCard}
            onError={onError}
            presence={presence}
            onPresence={sharePresence}
            me={config?.login ?? ""}
          />
        )}
        {board && view === "team" && (
          <TeamBoard
            board={board}
            selectedDate={selectedDate}
            onSelectDate={setSelectedDate}
            liveSprints={liveSprints}
            carryRequest={carryRequest}
            provider={provider}
            me={config?.login ?? ""}
            avatars={avatars}
            names={names}
            roster={roster}
            teamFilter={teamFilter}
            onSetFilter={setTeamFilter}
            customerFilter={customerFilter}
            onSetCustomerFilter={setCustomerFilter}
            onManageTeams={() => setManagingTeams(true)}
            onManageCustomers={() => setManagingCustomers(true)}
            patchCard={patchCard}
            addCard={addCard}
            replaceCard={replaceCard}
            removeCard={removeCard}
            reorderCards={reorderCards}
            presence={presence}
            onPresence={sharePresence}
            reload={reload}
            track={trackLoad}
            onError={onError}
            onOpen={openCard}
          />
        )}
      </main>

      {board && managingTeams && (
        <TeamsModal
          teams={roster}
          domains={board.domains}
          onAdd={addTeam}
          onRename={renameTeam}
          onRemove={removeTeam}
          onReorder={reorderTeams}
          capacities={Object.fromEntries(
            Object.entries(board.sprintStates).map(([t, st]) => [t, st.capacity?.points ?? 0]),
          )}
          onSetCapacity={(team, points) => {
            void provider
              .setTeamCapacity(team, points)
              .then(() => reload())
              .catch((err: unknown) => setError(errMessage(err)));
          }}
          onClose={() => setManagingTeams(false)}
        />
      )}

      {board && managingCustomers && (
        <CustomersModal
          customers={board.customers ?? []}
          provider={provider}
          onChanged={reload}
          onError={(m) => setError(m)}
          onClose={() => setManagingCustomers(false)}
        />
      )}

      {board && managingProjects && (
        <TeamsModal
          teams={board.projects}
          title="Manage projects"
          entity="project"
          domains={board.domains}
          onAdd={addProject}
          onRename={renameProject}
          onRemove={deleteProject}
          onReorder={reorderProjects}
          onClose={() => setManagingProjects(false)}
        />
      )}

      {board && detailCard && (
        <CardDetail
          card={board.cards.find((c) => c.itemId === detailCard.itemId) ?? detailCard}
          board={board}
          provider={provider}
          onClose={closeDetail}
          reload={reload}
          patchCard={patchCard}
          addCard={addCard}
          removeCard={removeCard}
          onReveal={revealOnRoadmap}
          onOpenCard={openWithin}
          onBack={detailStack.length > 0 ? backInDetail : undefined}
          cardLinkFor={(uid) => cardLink(window.location.origin, window.location.pathname, view, uid, selectedDate, todayIso())}
          onError={onError}
          onCustomerAdded={(cu) =>
            setBoard((cur) =>
              cur && !(cur.customers ?? []).some((c) => c.id === cu.id)
                ? { ...cur, customers: [...(cur.customers ?? []), cu] }
                : cur,
            )
          }
        />
      )}

    </div>
  );
}
