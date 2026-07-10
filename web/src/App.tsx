import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { clientId, fetchConfig, type AppConfig } from "./api/client";
import { apiProvider } from "./providers/api/apiProvider";
import {
  resourceToCard,
  sprintStateFrom,
  type CardResource,
  type OrderingResource,
  type SprintResource,
  type WatchFrame,
  type PresenceResource,
} from "./api/resources";
import type { Board, Card as CardModel } from "./providers/types";
import { MeBoard } from "./components/MeBoard";
import { TeamBoard } from "./components/TeamBoard";
import { CardDetail } from "./components/CardDetail";
import { Logo } from "./components/Logo";
import { fetchUsers, type GhUser } from "./users";
import { queryString, viewQueries, watchQuery } from "./viewquery";
import { todayIso } from "./date";
import { mergeNotes } from "./notes";
import { AppearanceMenu } from "./components/AppearanceMenu";
import { applyAppearance, persistAppearance, readAppearance, type Appearance } from "./theme";

type ViewMode = "me" | "team";

const LS_OWNER = "aeman.owner";
const LS_PROJECT = "aeman.project";
const LS_VIEW = "aeman.view";
const LS_TEAM_ROSTER = "aeman.teamRoster";
const LS_TEAM_FILTER = "aeman.teamFilter";
const LS_VIEW_AS = "aeman.viewAs";

function readView(): ViewMode {
  const raw = localStorage.getItem(LS_VIEW);
  if (raw === "team" || raw === "nixon") {
    return "team";
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

  const [owner, setOwner] = useState<string>(() => localStorage.getItem(LS_OWNER) ?? "");
  const [project, setProject] = useState<string>(
    () => localStorage.getItem(LS_PROJECT) ?? "",
  );
  const [view, setView] = useState<ViewMode>(readView);

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
  const [users, setUsers] = useState<Record<string, GhUser>>({});
  const fetchedUsers = useRef<Set<string>>(new Set());
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
  // Server-side writes not yet confirmed by GitHub (the write-behind queue),
  // fed by Queue watch frames; shown as a small counter that trends to zero.
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
  const [addedTeams, setAddedTeams] = useState<string[]>(
    () => readStringList(LS_TEAM_ROSTER) ?? [],
  );
  // Team filter: null = all, else the selected groups ("" = no-team). Multi-select
  // — Shift-click a chip to add/remove it.
  const [teamFilter, setTeamFilter] = useState<string[] | null>(() => {
    const v = localStorage.getItem(LS_TEAM_FILTER);
    if (!v) {
      return null;
    }
    try {
      const arr: unknown = JSON.parse(v);
      return Array.isArray(arr) && arr.length ? (arr as string[]) : null;
    } catch {
      return [v]; // legacy single-value filter
    }
  });

  // Bootstrap: fetch config and seed owner/project from localStorage or defaults.
  useEffect(() => {
    let cancelled = false;
    fetchConfig()
      .then((cfg) => {
        if (cancelled) {
          return;
        }
        setConfig(cfg);
        setOwner((cur) => cur || cfg.defaultOwner || "");
        setProject((cur) => cur || (cfg.defaultProject ? String(cfg.defaultProject) : ""));
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
    for (const t of addedTeams) {
      if (!seen.has(t)) {
        seen.add(t);
        out.push(t);
      }
    }
    // The board's own roster (teams with a sprint pointer) is the source of
    // truth — cards now load one view at a time, so we can't infer teams from
    // them. Any team present on the loaded cards is folded in as a fallback.
    for (const t of [...(board?.teams ?? []), ...(board?.cards ?? []).map((c) => c.team ?? "")]) {
      if (t && !seen.has(t)) {
        seen.add(t);
        out.push(t);
      }
    }
    return out;
  }, [addedTeams, board]);

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

  // Reorder the whole roster (from the manage dialog) and persist the order.
  const reorderTeams = useCallback((ordered: string[]) => {
    setAddedTeams(ordered);
    writeStringList(LS_TEAM_ROSTER, ordered);
  }, []);

  const addTeam = useCallback((team: string) => {
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
  }, []);

  const removeTeam = useCallback((team: string) => {
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
  }, []);

  // Persist the filter whenever it changes (null means "all", we store nothing).
  useEffect(() => {
    if (teamFilter === null) {
      localStorage.removeItem(LS_TEAM_FILTER);
    } else {
      localStorage.setItem(LS_TEAM_FILTER, JSON.stringify(teamFilter));
    }
  }, [teamFilter]);

  const doLoad = useCallback(
    async (ownerArg: string, numberArg: number) => {
      beginLoad();
      setError(null);
      try {
        // Identity + sprints AND the active view's cards together, swapped in
        // as one board: a reload() must never leave the board empty while the
        // cards are still in flight (loadBoard itself carries no cards).
        const addr = { owner: ownerArg, number: numberArg };
        const [loaded, lists] = await Promise.all([
          provider.loadBoard(ownerArg, numberArg),
          Promise.all(
            activeQueriesRef.current.map((q) => provider.listCards(addr, q)),
          ),
        ]);
        setBoard({ ...loaded, cards: mergeCardLists(lists) });
      } catch (err: unknown) {
        setError(errMessage(err));
      } finally {
        endLoad();
      }
    },
    [provider, beginLoad, endLoad],
  );

  // Load the active view's cards whenever the selection (view/day/teams) changes
  // or the board is (re)loaded. loadBoard brings only identity + sprints; the
  // cards for one view arrive here, so the UI holds just what it shows.
  const activeQueriesRef = useRef(activeQueries);
  activeQueriesRef.current = activeQueries;
  const bOwner = board?.owner;
  const bNumber = board?.number;
  useEffect(() => {
    if (!bOwner || bNumber == null) {
      return;
    }
    let cancelled = false;
    const addr = { owner: bOwner, number: bNumber };
    beginLoad();
    Promise.all(activeQueriesRef.current.map((q) => provider.listCards(addr, q)))
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
  }, [bOwner, bNumber, activeKey, provider, beginLoad, endLoad]);

  // Locked board: auto-load the pinned project once authenticated. A user whose
  // token can't read it just gets a load error (the access-denied placeholder).
  const lockLoadAttempted = useRef(false);
  useEffect(() => {
    if (
      config?.lockBoard &&
      config.authenticated &&
      config.defaultOwner &&
      config.defaultProject &&
      !lockLoadAttempted.current
    ) {
      lockLoadAttempted.current = true;
      setOwner(config.defaultOwner);
      setProject(String(config.defaultProject));
      void doLoad(config.defaultOwner, config.defaultProject);
    }
  }, [config, doLoad]);

  // Fetch GitHub name + avatar for any assignee not looked up yet. Watching the
  // board also covers people newly assigned to a card during the session.
  useEffect(() => {
    if (!board) {
      return;
    }
    const todo: string[] = [];
    for (const c of board.cards) {
      for (const a of c.assignees) {
        if (a && !fetchedUsers.current.has(a)) {
          fetchedUsers.current.add(a);
          todo.push(a);
        }
      }
    }
    if (todo.length === 0) {
      return;
    }
    void fetchUsers(todo)
      .then((u) => setUsers((cur) => ({ ...cur, ...u })))
      .catch(() => {});
  }, [board]);

  const handleLoad = () => {
    const trimmedOwner = owner.trim();
    const number = Number(project);
    if (!trimmedOwner || !Number.isFinite(number) || number <= 0) {
      setError("Enter an owner and a positive project number");
      return;
    }
    localStorage.setItem(LS_OWNER, trimmedOwner);
    localStorage.setItem(LS_PROJECT, String(number));
    void doLoad(trimmedOwner, number);
  };

  const reload = useCallback(() => {
    if (board) {
      void doLoad(board.owner, board.number);
    }
  }, [board, doLoad]);

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
  const boardLoaded = board !== null;
  const watchOwner = board?.owner;
  const watchProject = board?.number;
  useEffect(() => {
    if (!boardLoaded || !watchOwner || watchProject == null) {
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
      // The write-behind queue's depth: changes applied everywhere but not
      // yet confirmed by GitHub.
      if (frame.kind === "Queue") {
        queuePendingSync((frame.object as { pending?: number })?.pending ?? 0);
        return;
      }
      // A write GitHub finally rejected: the board has been rolled back to
      // the server's reloaded state; surface what was lost.
      if (frame.kind === "SyncError") {
        const msg = (frame.object as { message?: string })?.message;
        setError(msg || "a change could not be written to GitHub");
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
            .listLog({ owner: watchOwner, number: watchProject }, card.itemId)
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
      const url = `${proto}//${window.location.host}/api/v1/watch?owner=${encodeURIComponent(
        watchOwner,
      )}&project=${watchProject}&client=${clientId}&${watchKey}`;
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
    watchOwner,
    watchProject,
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
              .patchCard(cur, card.itemId, { team: t })
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
            <span className="version">v{config.version}</span>
          </div>
        </header>
        <div className="signin">
          <h2>Sign in to aeman</h2>
          <p>Connect your GitHub account to load and manage your project boards.</p>
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
          {config && <span className="version">v{config.version}</span>}
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
        {!config?.lockBoard && (
          <>
            <label className="field">
              <span>Owner</span>
              <input
                type="text"
                value={owner}
                placeholder="org-or-user"
                onChange={(e) => setOwner(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleLoad()}
              />
            </label>
            <label className="field">
              <span>Project #</span>
              <input
                type="number"
                min={1}
                value={project}
                placeholder="1"
                onChange={(e) => setProject(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleLoad()}
              />
            </label>
            <button
              type="button"
              className="btn btn-primary"
              onClick={handleLoad}
              disabled={loading}
            >
              {loading ? "Loading…" : "Load"}
            </button>
          </>
        )}

        {pendingSync > 0 && (
          <span
            className="sync-badge"
            title={`${pendingSync} change(s) applied but not yet written to GitHub`}
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
        </div>
      </div>

      <main className="content">
        {!board && !loading && (
          <p className="placeholder">
            {config?.lockBoard
              ? error
                ? "You don't have access to this board."
                : "Loading…"
              : "Enter an owner and project number, then press Load."}
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
            users={users}
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
                  .setPresence(board, config.login, card)
                  .catch(() => {});
              }
            }}
          />
        )}
        {board && view === "team" && (
          <TeamBoard
            board={board}
            selectedDate={selectedDate}
            onSelectDate={setSelectedDate}
            provider={provider}
            me={config?.login ?? ""}
            users={users}
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
