import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { clientId, fetchConfig, fetchHealth, type AppConfig } from "./api/client";
import { apiProvider } from "./providers/api/apiProvider";
import { guardSignedOut } from "./session";
import { mergeCardLists } from "./cardmerge";
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
import { avatarsFrom, namesFrom } from "./users";
import { forgeCopy } from "./forge";
import { unpushedNotice, type HealthStatus } from "./health";
import { migrateBoardScopedKeys } from "./storage";
import { pruneTeamFilter, settlePendingTeams, teamRoster } from "./teams";
import { queryString, viewQueries, watchQueries } from "./viewquery";
import { PersonalDialog } from "./components/PersonalDialog";
import { todayIso, setBoardTimezone } from "./date";
import { mergeNotes } from "./notes";
import { nameConflict } from "./names";
import { AppearanceMenu } from "./components/AppearanceMenu";
import { applyAppearance, persistAppearance, readAppearance, type Appearance } from "./theme";

type ViewMode = "me" | "team" | "project" | "process";

const LS_VIEW = "aeman.view";
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
  // Display names come with the roster too (a GitLab board has them, a GitHub
  // one does not); they decorate labels only — the login stays the identifier.
  const names = useMemo(() => namesFrom(board?.members ?? []), [board?.members]);
  // The forge (GitHub / GitLab) spells the sign-in and token copy; before the
  // config answers, and on an older server, that is GitHub.
  const forge = useMemo(() => forgeCopy(config), [config]);
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
  const [presence, setPresenceMap] = useState<Record<string, string>>({});
  const [detailCard, setDetailCard] = useState<CardModel | null>(null);

  // Teams are the server's (board.teams). The only team state here is the
  // just-added ones whose create is in flight — shown at once, and gone from
  // this list as soon as the board declares them (see teams.ts). Nothing of it
  // is persisted.
  const [pendingTeams, setPendingTeams] = useState<string[]>([]);
  // Team filter: null = all, else the selected groups ("" = no-team). Multi-select
  // — Shift-click a chip to add/remove it. Persisted, and pruned to the teams
  // the board has once it is loaded.
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
  const provider = useMemo(() => guardSignedOut(apiProvider, onSignedOut), [onSignedOut]);

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
  useEffect(() => {
    if (!boardLoaded) {
      return;
    }
    setTeamFilter((cur) => pruneTeamFilter(cur, roster));
  }, [boardLoaded, roster]);

  // What the active board loads and watches: Me is personal (the server fills in
  // "who am I" unless view-as impersonates someone), Team names the teams it
  // shows (the filter, or the whole roster) and loads the day grid PLUS the
  // weekly plan. activeKey / watchKey are stable serialisations used to
  // re-fetch and re-subscribe only when the selection actually changes.
  // A linked personal board rides beside the Me view: fetched and watched
  // with it (its own selector, its own socket), never while impersonating.
  const hasPersonal = board?.personal !== undefined;
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
        hasPersonal,
      ),
    [view, selectedDate, teamFilter, roster, viewAs, hasPersonal],
  );
  const activeKey = activeQueries.map(queryString).join("|");
  const watchKeys = useMemo(
    () =>
      watchQueries(
        view,
        selectedDate,
        teamFilter ?? [...new Set([...roster, ""])],
        viewAs ?? undefined,
        hasPersonal,
      ).map(queryString),
    [view, selectedDate, teamFilter, roster, viewAs, hasPersonal],
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
      setBoard((cur) => ({
        ...loaded,
        cards: mergeCardLists(lists, cur?.cards),
        processes,
      }));
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
        // The listing is the row view; the notes, events and bodies already
        // fetched are kept, so switching views costs one request, not one
        // per card all over again.
        setBoard((cur) =>
          cur ? { ...cur, cards: mergeCardLists(lists, cur.cards) } : cur,
        );
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
  // The day feed (MeBoard) subscribes here to hear about Card watch frames —
  // a foreign note or move must land in the feed without a reload. Own
  // mutations never arrive (the watch is keyed by ?client=, and the server
  // skips this tab's echoes), so every frame relayed is someone else's.
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
    // One socket per selector (Me + its personal board). Any of them dropping
    // rebuilds the whole set after a re-LIST, so the two stay in step.
    const sockets: WebSocket[] = [];
    let closed = false;
    let retry: number | undefined;
    const closeAll = () => {
      for (const s of sockets.splice(0)) {
        s.close();
      }
    };
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
        const notifyCardFrame = (deleted: boolean) => {
          for (const fn of cardFrameListeners.current) {
            fn(card.itemId, deleted);
          }
        };
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
      // Scope the watch to the active view: Me watches its day selection (and
      // the personal board on a second socket), Team watches every card of the
      // teams it shows (grid + weekly plan). A card entering the selection
      // arrives as ADDED, one leaving as DELETED. ?client= keeps our own
      // mutations from echoing back. Re-subscribes when watchKey changes (a
      // dep below).
      for (const key of watchKey.split("|")) {
        const url = `${proto}//${window.location.host}/api/v1/watch?client=${clientId}&${key}`;
        const socket = new WebSocket(url);
        sockets.push(socket);
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
          // A socket we closed ourselves (teardown, or the rebuild below) is
          // no longer in the set and must not schedule another rebuild.
          if (closed || !sockets.includes(socket) || retry !== undefined) {
            return;
          }
          retry = window.setTimeout(() => {
            retry = undefined;
            closeAll();
            reloadRef.current();
            connect();
          }, 3000);
        });
      }
    };
    connect();
    return () => {
      closed = true;
      window.clearTimeout(retry);
      closeAll();
    };
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

  // The personal board: linked from the user menu through a small dialog,
  // unlinked from the same menu after a confirm. Either way the board reloads
  // — its metadata carries the link, and the Me fetch follows it.
  const [personalDialog, setPersonalDialog] = useState(false);
  const linkPersonal = useCallback(
    async (url: string) => {
      await provider.linkPersonal(url);
      reload();
    },
    [provider, reload],
  );
  const unlinkPersonal = useCallback(() => {
    if (
      !window.confirm(
        "Unlink your personal board? The repository itself is left untouched.",
      )
    ) {
      return;
    }
    void provider
      .unlinkPersonal()
      .then(() => reload())
      .catch((err: unknown) => setError(errMessage(err)));
  }, [provider, reload]);

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
          <a className="btn btn-primary signin-btn" href={config.authUrl ?? "/auth/login"}>
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
            personal={board ? (board.personal ?? null) : undefined}
            onLinkPersonal={() => setPersonalDialog(true)}
            onUnlinkPersonal={unlinkPersonal}
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
          {/* forge.noTokenHint, with the command set in <code>. */}
          <span>
            No {forge.label} token — run <code>{forge.cli} auth login</code> in the
            terminal where aeman runs.
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
            names={names}
            connectHint={forge.connectHint}
            subscribeCardFrames={subscribeCardFrames}
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
            names={names}
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
            names={names}
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

      {personalDialog && (
        <PersonalDialog
          onClose={() => setPersonalDialog(false)}
          onLink={linkPersonal}
          repoPlaceholder={forge.repoPlaceholder}
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
