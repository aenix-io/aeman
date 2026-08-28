import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { clientId, fetchConfig, fetchHealth, type AppConfig } from "./api/client";
import { apiProvider } from "./providers/api/apiProvider";
import {
  resourceToCard,
  sprintStateFrom,
  type BoardResource,
  type CardResource,
  type OrderingResource,
  type SprintResource,
  type WatchFrame,
  type PresenceResource,
} from "./api/resources";
import type { Board, Card as CardModel } from "./providers/types";
import { MeBoard } from "./components/MeBoard";
import { TeamBoard } from "./components/TeamBoard";
import { ProjectBoard } from "./components/ProjectBoard";
import { ProcessBoard } from "./components/ProcessBoard";
import { TeamsModal } from "./components/TeamsModal";
import { readProjectFilter, writeProjectFilter } from "./projectFilter";
import { boardMetadata, processesFrom } from "./providers/api/apiProvider";
import type { ProcessInfo } from "./providers/types";
import { CardDetail } from "./components/CardDetail";
import { Logo } from "./components/Logo";
import { avatarsFrom } from "./users";
import { unpushedNotice, type HealthStatus } from "./health";
import { migrateBoardScopedKeys } from "./storage";
import { queryString, viewQueries, watchQuery } from "./viewquery";
import { todayIso, setBoardTimezone } from "./date";
import { mergeNotes } from "./notes";
import { AppearanceMenu } from "./components/AppearanceMenu";
import { applyAppearance, persistAppearance, readAppearance, type Appearance } from "./theme";

type ViewMode = "me" | "team" | "project" | "process";

const LS_VIEW = "aeman.view";
const LS_TEAM_ROSTER = "aeman.teamRoster";
const LS_TEAM_FILTER = "aeman.teamFilter";
const LS_VIEW_AS = "aeman.viewAs";

function readView(): ViewMode {
  const raw = localStorage.getItem(LS_VIEW);
  if (raw === "team" || raw === "nixon") {
    return "team";
  }
  // "plan" is the tab's pre-rename name — an existing localStorage entry
  // still lands on the Project board rather than silently on Me.
  if (raw === "project" || raw === "plan") {
    return "project";
  }
  if (raw === "process") {
    return "process";
  }
  return "me";
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

function writeStringList(key: string, value: string[]) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // ignore persistence failures
  }
}

// The server serves one board, so the roster and filter live under plain keys.
// The picker era scoped them per board; the first load under this build moves
// the last board's values over (once — see storage.ts).
function settleStorage() {
  migrateBoardScopedKeys(localStorage, [LS_TEAM_ROSTER, LS_TEAM_FILTER]);
}

function readRoster(): string[] {
  settleStorage();
  return readStringList(LS_TEAM_ROSTER) ?? [];
}

function readFilter(): string[] | null {
  settleStorage();
  const v = localStorage.getItem(LS_TEAM_FILTER);
  if (!v) {
    return null;
  }
  try {
    const arr: unknown = JSON.parse(v);
    return Array.isArray(arr) && arr.length ? (arr as string[]) : null;
  } catch {
    return null;
  }
}

const errMessage = (err: unknown) =>
  err instanceof Error ? err.message : String(err);

// mergeCardLists flattens a view's lists (e.g. the Team grid + its weekly
// plan), deduping by item id; board order within each list is preserved.
function mergeCardLists(lists: CardModel[][]): CardModel[] {
  const seen = new Set<string>();
  return lists.flat().filter((c) => {
    if (seen.has(c.itemId)) {
      return false;
    }
    seen.add(c.itemId);
    return true;
  });
}

export function App() {
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [tokenWarningDismissed, setTokenWarningDismissed] = useState(false);

  const [view, setView] = useState<ViewMode>(readView);
  // The project chips' selection, shared by the Project and Process tabs.
  const [projectFilter, setProjectFilterState] = useState<string[] | null>(readProjectFilter);
  const setProjectFilter = (keys: string[] | null) => {
    setProjectFilterState(keys);
    writeProjectFilter(keys);
  };
  // The project manager: one dialog, reached from the chip row of both the
  // Project and the Process tab. Its writes return as soon as the server's
  // cache has them; the Board watch frame repaints — nothing reloads.
  const [managingProjects, setManagingProjects] = useState(false);
  // `domain` is the repository the project is declared in (multi-domain
  // boards offer the choice; otherwise the server picks the primary).
  const addProject = (name: string, domain?: string) => {
    if (!board || !name.trim()) {
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
  const [selectedDate, setSelectedDate] = useState<string>(todayIso());
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
  // Count of in-flight data loads (initial load + per-view card fetches);
  // any of them showing keeps the top progress bar visible.
  const [pendingLoads, setPendingLoads] = useState(0);
  const loading = pendingLoads > 0;
  const beginLoad = useCallback(() => setPendingLoads((n) => n + 1), []);
  const endLoad = useCallback(() => setPendingLoads((n) => n - 1), []);
  // Boards wrap their slow server calls (carry over etc.) with this so the
  // progress bar covers the operation itself, not just the refetch after it.
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
  const [error, setError] = useState<string | null>(null);
  // Live selections of other users (login -> card uid), fed by Presence
  // watch frames; purely ephemeral shared-cursor state.
  const [presence, setPresenceMap] = useState<Record<string, string>>({});
  const [detailCard, setDetailCard] = useState<CardModel | null>(null);

  // Team roster + filter, persisted in localStorage. The roster is the union of
  // the teams found on the board and any teams the user has added by hand; the
  // filter is the subset of the roster currently shown (defaults to everything).
  const [addedTeams, setAddedTeams] = useState<string[]>(readRoster);
  // Team filter: null = all, else the selected groups ("" = no-team). Multi-select
  // — Shift-click a chip to add/remove it.
  const [teamFilter, setTeamFilter] = useState<string[] | null>(readFilter);

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
        setSelectedDate(todayIso());
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

  const provider = apiProvider;

  // The roster: user-arranged teams first (in their saved order), then any team
  // present on the board that isn't in that list yet. No alphabetical sort, so a
  // hand-picked order sticks.
  const roster = useMemo(() => {
    const seen = new Set<string>();
    const out: string[] = [];
    // The board's own teams come FIRST, in the server-side order (the
    // sprint-state cards' positions) — shared by everyone, on every device.
    for (const t of board?.teams ?? []) {
      if (t && !seen.has(t)) {
        seen.add(t);
        out.push(t);
      }
    }
    // Hand-added drafts (no sprint pointer yet) follow in their local order,
    // then any team present only on loaded cards as a fallback.
    for (const t of [...addedTeams, ...(board?.cards ?? []).map((c) => c.team ?? "")]) {
      if (t && !seen.has(t)) {
        seen.add(t);
        out.push(t);
      }
    }
    return out;
  }, [addedTeams, board]);

  // A saved filter can outlive its teams (a team renamed away, or stale
  // data): entries no team backs would silently blank the board, so prune
  // them — and an emptied filter means "all" again. Only once the board is
  // loaded: before that the roster is the saved list alone.
  const boardLoaded = board !== null;
  useEffect(() => {
    if (!boardLoaded) {
      return;
    }
    setTeamFilter((cur) => {
      if (!cur) {
        return cur;
      }
      const next = cur.filter((t) => t === "" || roster.includes(t));
      if (next.length === cur.length) {
        return cur;
      }
      return next.length ? next : null;
    });
  }, [boardLoaded, roster]);

  // What the active board loads and watches: Me is personal (the server fills in
  // "who am I" unless view-as impersonates someone), Team names the teams it
  // shows (the filter, or the whole roster) and loads the day grid PLUS the
  // weekly plan. activeKey / watchKey are stable serialisations used to
  // re-fetch and re-subscribe only when the selection actually changes.
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
      ),
    [view, selectedDate, teamFilter, roster, viewAs],
  );
  const activeKey = activeQueries.map(queryString).join("|");
  const watchSel = useMemo(
    () =>
      watchQuery(
        view,
        selectedDate,
        teamFilter ?? [...new Set([...roster, ""])],
        viewAs ?? undefined,
      ),
    [view, selectedDate, teamFilter, roster, viewAs],
  );
  const watchKey = queryString(watchSel);

  // Reorder the whole roster (from the manage dialog). Teams the server knows
  // (sprint pointer) get the order pushed to the board itself — their hidden
  // sprint-state cards are moved, so the order is shared by the whole team.
  // Hand-added drafts keep their relative order locally as before.
  const reorderTeams = useCallback(
    (ordered: string[]) => {
      setAddedTeams(ordered);
      writeStringList(LS_TEAM_ROSTER, ordered);
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

  // Add a team: shown at once from the local list, and declared on the server
  // as a sprint pointer with no dates yet — `domain` picks the repository its
  // roster entry is written to (the primary unless chosen). The Board frame
  // then carries it as one of the board's own teams.
  const addTeam = useCallback(
    (team: string, domain?: string) => {
      const t = team.trim();
      if (!t) {
        return;
      }
      setAddedTeams((cur) => {
        if (cur.includes(t)) {
          return cur;
        }
        const next = [...cur, t];
        writeStringList(LS_TEAM_ROSTER, next);
        return next;
      });
      if (board && !board.teams.includes(t)) {
        void provider
          .setSprintState(t, null, null, domain)
          .catch((err: unknown) => setError(errMessage(err)));
      }
    },
    [board, provider],
  );

  const removeTeam = useCallback(
    (team: string) => {
      const dropLocal = () => {
        setAddedTeams((cur) => {
          const next = cur.filter((t) => t !== team);
          writeStringList(LS_TEAM_ROSTER, next);
          return next;
        });
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
        // A hand-added draft lives only in this browser.
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

  const doLoad = useCallback(async () => {
    beginLoad();
    setError(null);
    try {
      // Identity + sprints AND the active view's cards together, swapped in
      // as one board: a reload() must never leave the board empty while the
      // cards are still in flight (loadBoard itself carries no cards).
      const [loaded, lists, processes] = await Promise.all([
        provider.loadBoard(),
        Promise.all(activeQueriesRef.current.map((q) => provider.listCards(q))),
        // The process structure rides with the board from the start; the
        // Board watch frame keeps it current afterwards.
        provider.listProcesses().catch(() => [] as ProcessInfo[]),
      ]);
      setBoard({ ...loaded, cards: mergeCardLists(lists), processes });
    } catch (err: unknown) {
      setError(errMessage(err));
    } finally {
      endLoad();
    }
  }, [provider, beginLoad, endLoad]);

  // Load the active view's cards whenever the selection (view/day/teams) changes
  // or the board is (re)loaded. loadBoard brings only identity + sprints; the
  // cards for one view arrive here, so the UI holds just what it shows.
  const activeQueriesRef = useRef(activeQueries);
  activeQueriesRef.current = activeQueries;
  useEffect(() => {
    if (!boardLoaded) {
      return;
    }
    let cancelled = false;
    beginLoad();
    Promise.all(activeQueriesRef.current.map((q) => provider.listCards(q)))
      .then((lists) => {
        if (cancelled) {
          return;
        }
        setBoard((cur) => (cur ? { ...cur, cards: mergeCardLists(lists) } : cur));
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(errMessage(err));
        }
      })
      .finally(endLoad);
    return () => {
      cancelled = true;
    };
  }, [boardLoaded, activeKey, provider, beginLoad, endLoad]);

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
          return { ...c, ...p };
        });
        return changed ? { ...cur, cards } : cur;
      });
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
  const reorderCards = useCallback((orderedIds: string[]) => {
    setBoard((cur) => {
      if (!cur) {
        return cur;
      }
      const rank = new Map(orderedIds.map((id, i) => [id, i]));
      const ranked = cur.cards
        .filter((c) => rank.has(c.itemId))
        .sort((a, b) => rank.get(a.itemId)! - rank.get(b.itemId)!);
      const rest = cur.cards.filter((c) => !rank.has(c.itemId));
      return { ...cur, cards: [...ranked, ...rest] };
    });
  }, []);

  // Live updates: the server pushes resource events over an unscoped WebSocket
  // watch (Kubernetes style). We LIST via loadBoard, then mirror the ADDED /
  // MODIFIED / DELETED frames: Card frames upsert/remove by uid, Sprint frames
  // update the team's pointer, Ordering frames re-sort the local cards. On a
  // socket drop we re-LIST after reconnecting to reconcile anything missed.
  // Refs keep the socket from being rebuilt on every board change.
  const reloadRef = useRef(reload);
  reloadRef.current = reload;
  const boardRef = useRef(board);
  boardRef.current = board;
  useEffect(() => {
    if (!boardLoaded) {
      return;
    }
    let socket: WebSocket | null = null;
    let closed = false;
    let retry: number | undefined;
    const applyFrame = (frame: WatchFrame) => {
      // Sync marks a finished server-side reload; the diff already arrived as
      // ordinary events, so there is nothing left to do here.
      if (frame.kind === "Sync") {
        return;
      }
      // The board's STRUCTURE changed — a project, a column, a deadline, a
      // process. The frame carries the board itself, so it is applied the way
      // a Card frame is: no round trip, and our own roster writes need no
      // reload either.
      if (frame.kind === "Board" && frame.object) {
        const obj = frame.object as BoardResource & { processes?: ProcessInfo[] | null };
        setBoard((cur) =>
          cur
            ? { ...cur, ...boardMetadata(obj), processes: processesFrom(obj.processes) }
            : cur,
        );
        return;
      }
      // The write queue's depth: changes applied everywhere but not yet
      // committed.
      if (frame.kind === "Queue") {
        queuePendingSync((frame.object as { pending?: number })?.pending ?? 0);
        return;
      }
      // A write the store finally rejected: the board has been rolled back to
      // the server's reloaded state; surface what was lost.
      if (frame.kind === "SyncError") {
        const msg = (frame.object as { message?: string })?.message;
        setError(msg || "a change could not be saved");
        return;
      }
      if (!frame.object) {
        return;
      }
      if (frame.kind === "Card") {
        const card = resourceToCard(frame.object as CardResource);
        if (frame.type === "DELETED") {
          removeCard(card.itemId);
          return;
        }
        // Note/event changes arrive as plain card changes, and the resource
        // carries neither: refetch the log when this card's was already loaded.
        const existing = boardRef.current?.cards.find(
          (c) => c.itemId === card.itemId,
        );
        addCard(card);
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
        const sprint = frame.object as SprintResource;
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
        reorderCards((frame.object as OrderingResource).spec.uids);
        return;
      }
      if (frame.kind === "Presence") {
        const { login, card } = frame.object as PresenceResource;
        if (!login) {
          return;
        }
        setPresenceMap((cur) => {
          const next = { ...cur };
          if (card) {
            next[login] = card;
          } else {
            delete next[login];
          }
          return next;
        });
      }
    };
    const connect = () => {
      // A fresh connection replays the presence and queue snapshots; drop
      // stale marks (the queue may have drained while we were away).
      setPresenceMap({});
      queuePendingSync(0);
      const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
      // Scope the watch to the active view: Me watches its day selection, Team
      // watches every card of the teams it shows (grid + weekly plan). A card
      // entering the selection arrives as ADDED, one leaving as DELETED.
      // ?client= keeps our own mutations from echoing back. Re-subscribes when
      // watchKey changes (a dep below).
      const url = `${proto}//${window.location.host}/api/v1/watch?client=${clientId}&${watchKey}`;
      socket = new WebSocket(url);
      socket.addEventListener("message", (e) => {
        let frame: WatchFrame;
        try {
          frame = JSON.parse(e.data as string) as WatchFrame;
        } catch {
          return;
        }
        applyFrame(frame);
      });
      socket.addEventListener("close", () => {
        if (closed) {
          return;
        }
        retry = window.setTimeout(() => {
          reloadRef.current();
          connect();
        }, 3000);
      });
    };
    connect();
    return () => {
      closed = true;
      window.clearTimeout(retry);
      socket?.close();
    };
  }, [
    boardLoaded,
    watchKey,
    provider,
    addCard,
    removeCard,
    patchCard,
    reorderCards,
    queuePendingSync,
  ]);

  const onError = useCallback((message: string) => setError(message), []);

  // Rename a team everywhere: the roster, the filter, and every card using it.
  const renameTeam = useCallback(
    (from: string, to: string) => {
      const t = to.trim();
      if (!t || t === from) {
        return;
      }
      setAddedTeams((cur) => {
        const next = [...new Set(cur.map((x) => (x === from ? t : x)))];
        writeStringList(LS_TEAM_ROSTER, next);
        return next;
      });
      setTeamFilter((cur) =>
        cur === null ? cur : cur.map((k) => (k === from ? t : k)),
      );
      setBoard((cur) => {
        if (!cur) {
          return cur;
        }
        for (const card of cur.cards) {
          if (card.team === from) {
            void provider
              .patchCard(card.itemId, { team: t })
              .catch((err: unknown) => {
                patchCard(card.itemId, { team: from });
                setError(errMessage(err));
              });
          }
        }
        return {
          ...cur,
          cards: cur.cards.map((c) => (c.team === from ? { ...c, team: t } : c)),
        };
      });
    },
    [patchCard, provider],
  );

  const showTokenWarning =
    config !== null && !config.tokenAvailable && !tokenWarningDismissed;

  // OAuth mode: gate the whole UI behind a GitHub sign-in.
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
          <h2>Sign in to aeman</h2>
          <p>Connect your GitHub account to open the board.</p>
          <a className="btn btn-primary signin-btn" href={config.authUrl ?? "/auth/login"}>
            Sign in with GitHub
          </a>
        </div>
      </div>
    );
  }

  return (
    <div className="app">
      {loading && (
        <div className="loading-bar" role="progressbar" aria-label="Loading data" />
      )}
      <header className="app-header">
        <div className="brand">
          <Logo className="brand-logo" />
          {config && <span className="version">{config.version}</span>}
        </div>
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
          <span>
            No GitHub token — run <code>gh auth login</code> in the terminal where aeman
            runs.
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
        {board && (
          <span className="board-title" title={board.url || undefined}>
            {board.title}
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
            onClick={() => setView("me")}
          >
            Me
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={view === "team"}
            className={`segment${view === "team" ? " segment-active" : ""}`}
            onClick={() => setView("team")}
          >
            Team
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={view === "project"}
            className={`segment${view === "project" ? " segment-active" : ""}`}
            onClick={() => setView("project")}
          >
            Project
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={view === "process"}
            className={`segment${view === "process" ? " segment-active" : ""}`}
            onClick={() => setView("process")}
          >
            Process
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
            onSelectDate={setSelectedDate}
            viewAs={viewAs}
            onViewAs={setViewAsPersisted}
            provider={provider}
            me={config?.login ?? ""}
            avatars={avatars}
            teams={roster}
            teamFilter={teamFilter}
            onSetFilter={setTeamFilter}
            onAddTeam={addTeam}
            onRemoveTeam={removeTeam}
            onRenameTeam={renameTeam}
            patchCard={patchCard}
            addCard={addCard}
            replaceCard={replaceCard}
            removeCard={removeCard}
            reorderCards={reorderCards}
            reload={reload}
            onError={onError}
            onOpen={(c) => setDetailCard(c)}
            onPresence={(card) => {
              if (board && config?.login) {
                void provider
                  .setPresence(config.login, card)
                  .catch(() => {});
              }
            }}
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
            onOpen={(c) => setDetailCard(c)}
          />
        )}
        {board && view === "process" && (
          <ProcessBoard
            board={board}
            provider={provider}
            filter={projectFilter}
            onSetFilter={setProjectFilter}
            onManageProjects={() => setManagingProjects(true)}
            avatars={avatars}
            onError={onError}
          />
        )}
        {board && view === "team" && (
          <TeamBoard
            board={board}
            selectedDate={selectedDate}
            onSelectDate={setSelectedDate}
            provider={provider}
            me={config?.login ?? ""}
            avatars={avatars}
            roster={roster}
            teamFilter={teamFilter}
            onSetFilter={setTeamFilter}
            onAddTeam={addTeam}
            onRemoveTeam={removeTeam}
            onRenameTeam={renameTeam}
            onReorderTeams={reorderTeams}
            patchCard={patchCard}
            addCard={addCard}
            replaceCard={replaceCard}
            removeCard={removeCard}
            reorderCards={reorderCards}
            presence={presence}
            reload={reload}
            track={trackLoad}
            onError={onError}
            onOpen={(c) => setDetailCard(c)}
          />
        )}
      </main>

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
          onClose={() => setDetailCard(null)}
          reload={reload}
          patchCard={patchCard}
        />
      )}

    </div>
  );
}
