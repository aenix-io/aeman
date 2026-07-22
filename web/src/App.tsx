import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { fetchConfig, type AppConfig } from "./api/client";
import { apiProvider } from "./providers/api/apiProvider";
import type { Card as CardModel } from "./providers/types";
import { MeBoard } from "./components/MeBoard";
import { TeamBoard } from "./components/TeamBoard";
import { CardDetail } from "./components/CardDetail";
import { PersonalDialog } from "./components/PersonalDialog";
import { Logo } from "./components/Logo";
import { fetchUsers, type GhUser } from "./users";
import { queryString, viewQueries, watchQuery } from "./viewquery";
import { todayIso, setBoardTimezone } from "./date";
import { AppearanceMenu } from "./components/AppearanceMenu";
import { applyAppearance, persistAppearance, readAppearance, type Appearance } from "./theme";
import { useBoardData } from "./useBoardData";
import {
  clearPersonalBoard,
  nextPane,
  personalPaneVisible,
  prevPane,
  readPersonalBoard,
  samePointer,
  savePersonalBoard,
  type MePane,
  type PersonalPointer,
} from "./personal";

type ViewMode = "me" | "team";

const LS_OWNER = "aeman.owner";
const LS_PROJECT = "aeman.project";
const LS_VIEW = "aeman.view";
const LS_TEAM_ROSTER = "aeman.teamRoster";
const LS_TEAM_FILTER = "aeman.teamFilter";
const LS_VIEW_AS = "aeman.viewAs";
const LS_PERSONAL_TEAM = "aeman.personalTeam";

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

// The team roster and filter are BOARD-scoped: loading board 37 must not
// inherit board 36's teams. The pre-scoping unscoped keys are ignored — they
// can't be attributed to any particular board — so every board starts from
// its own saved state (or clean).
const scopedLS = (base: string, boardKey: string) => `${base}.${boardKey}`;

function readRosterFor(boardKey: string): string[] {
  return readStringList(scopedLS(LS_TEAM_ROSTER, boardKey)) ?? [];
}

function readFilterFor(boardKey: string): string[] | null {
  const v = localStorage.getItem(scopedLS(LS_TEAM_FILTER, boardKey));
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
  const [error, setError] = useState<string | null>(null);
  // A failing personal board must never take down the work board: its
  // problems land in this separate, dismissible warning instead of `error`.
  const [personalError, setPersonalError] = useState<string | null>(null);
  // Which board the open card-detail dialog belongs to: mutations and log
  // fetches from the dialog must hit the card's own project.
  const [detail, setDetail] = useState<{ card: CardModel; src: "work" | "personal" } | null>(null);

  // Team roster + filter, persisted in localStorage PER BOARD. The roster is
  // the union of the teams found on the board and any teams the user has added
  // by hand; the filter is the subset of the roster currently shown (defaults
  // to everything). Both are seeded for the last-used board and swapped by
  // doLoad when another board is opened.
  const boardKeyRef = useRef(
    `${localStorage.getItem(LS_OWNER) ?? ""}/${localStorage.getItem(LS_PROJECT) ?? ""}`,
  );
  const [addedTeams, setAddedTeams] = useState<string[]>(() =>
    readRosterFor(boardKeyRef.current),
  );
  // Team filter: null = all, else the selected groups ("" = no-team). Multi-select
  // — Shift-click a chip to add/remove it.
  const [teamFilter, setTeamFilter] = useState<string[] | null>(() =>
    readFilterFor(boardKeyRef.current),
  );

  // ------------------------------------------------------------ personal board
  // The pointer to the user's own private project, per login (see personal.ts).
  const login = config?.login ?? "";
  const [personalPtr, setPersonalPtr] = useState<PersonalPointer | null>(null);
  useEffect(() => {
    setPersonalPtr(login ? readPersonalBoard(localStorage, login) : null);
  }, [login]);
  const [personalOpen, setPersonalOpen] = useState(false);
  // Which Me pane is active on narrow screens (wide shows both side by side).
  const [mePane, setMePane] = useState<MePane>("work");
  // Team view pointed at the personal board (the virtual "Personal" chip).
  const [personalTeam, setPersonalTeam] = useState<boolean>(
    () => localStorage.getItem(LS_PERSONAL_TEAM) === "1",
  );
  const setPersonalTeamPersisted = useCallback((on: boolean) => {
    setPersonalTeam(on);
    if (on) {
      localStorage.setItem(LS_PERSONAL_TEAM, "1");
    } else {
      localStorage.removeItem(LS_PERSONAL_TEAM);
    }
  }, []);

  const attachPersonal = useCallback(
    (ptr: PersonalPointer) => {
      if (!login) {
        return;
      }
      savePersonalBoard(localStorage, login, ptr);
      setPersonalPtr(ptr);
      setPersonalError(null);
    },
    [login],
  );
  const detachPersonal = useCallback(() => {
    if (login) {
      clearPersonalBoard(localStorage, login);
    }
    setPersonalPtr(null);
    setPersonalTeamPersisted(false);
    setMePane("work");
    setPersonalError(null);
    // An open personal card detail must not outlive its board (its mutations
    // would have nowhere correct to go).
    setDetail((cur) => (cur?.src === "personal" ? null : cur));
  }, [login, setPersonalTeamPersisted]);

  // Bootstrap: fetch config and seed owner/project from localStorage or defaults.
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
  const onError = useCallback((message: string) => setError(message), []);
  const onPersonalError = useCallback(
    (message: string) => setPersonalError(message),
    [],
  );

  // ------------------------------------------------------- the two board slots
  // The work board and the personal board are two independent instances of the
  // same machinery: each has its own state, its own scoped watch socket, its
  // own server address — so every mutation, note and log call naturally hits
  // the right project without any per-card routing.

  const workRef = useRef<ReturnType<typeof useBoardData> | null>(null);

  // The roster: user-arranged teams first (in their saved order), then any team
  // present on the board that isn't in that list yet. No alphabetical sort, so a
  // hand-picked order sticks. (Depends on work.board; declared after the hook.)

  // What the work board loads and watches: Me is personal (the server fills in
  // "who am I" unless view-as impersonates someone), Team names the teams it
  // shows (the filter, or the whole roster) and loads the day grid PLUS the
  // weekly plan. The keys are stable serialisations used to re-fetch and
  // re-subscribe only when the selection actually changes.
  const [rosterForQueries, setRosterForQueries] = useState<string[]>([]);
  const activeQueries = useMemo(
    // No filter means ALL: the roster's teams plus the no-team group, so an
    // unfiltered Team board misses nothing (the client filter mirrors this —
    // teamFilter === null passes every card).
    () =>
      viewQueries(
        view,
        selectedDate,
        teamFilter ?? [...new Set([...rosterForQueries, ""])],
        viewAs ?? undefined,
      ),
    [view, selectedDate, teamFilter, rosterForQueries, viewAs],
  );
  const activeKey = activeQueries.map(queryString).join("|");
  const watchSel = useMemo(
    () =>
      watchQuery(
        view,
        selectedDate,
        teamFilter ?? [...new Set([...rosterForQueries, ""])],
        viewAs ?? undefined,
      ),
    [view, selectedDate, teamFilter, rosterForQueries, viewAs],
  );
  const watchKey = queryString(watchSel);

  const work = useBoardData({
    provider,
    queries: activeQueries,
    queriesKey: activeKey,
    watchKey,
    onError,
    beginLoad,
    endLoad,
  });
  workRef.current = work;
  const board = work.board;

  // The personal board always loads the me view of the selected day; when the
  // Team view is pointed at it (the "Personal" chip), it loads the team grid +
  // weekly plan of its no-team group instead. viewAs never applies: another
  // person's personal board is not reachable by design.
  const personalActiveView: ViewMode =
    view === "team" && personalTeam ? "team" : "me";
  const personalQueries = useMemo(
    () => viewQueries(personalActiveView, selectedDate, [""]),
    [personalActiveView, selectedDate],
  );
  const personalKey = personalQueries.map(queryString).join("|");
  const personalWatchKey = queryString(
    watchQuery(personalActiveView, selectedDate, [""]),
  );

  const personal = useBoardData({
    provider,
    queries: personalQueries,
    queriesKey: personalKey,
    watchKey: personalWatchKey,
    onError: onPersonalError,
    beginLoad,
    endLoad,
  });

  // (Re)load the personal board when attached; drop it when detached. A load
  // failure detaches nothing — the pointer survives a flaky morning — but the
  // pane renders only from a successfully loaded board.
  const personalAddr = personalPtr
    ? `${personalPtr.owner}/${personalPtr.number}`
    : null;
  const personalLoad = personal.load;
  const personalReset = personal.reset;
  useEffect(() => {
    // Reset FIRST, on every pointer change — not only on detach. Without this
    // a failed load of a NEW pointer leaves the PREVIOUS board's state alive:
    // the pane keeps rendering (and mutating!) the old project while the
    // toolbar already names the new one. Found by live testing (attach #1,
    // repoint to a nonexistent #99: the #1 pane survived the failure).
    personalReset();
    if (!personalAddr || !login || config?.lockBoard) {
      return;
    }
    const [pOwner, pNumber] = [
      personalAddr.slice(0, personalAddr.lastIndexOf("/")),
      Number(personalAddr.slice(personalAddr.lastIndexOf("/") + 1)),
    ];
    let cancelled = false;
    personalLoad(pOwner, pNumber).catch((err: unknown) => {
      if (!cancelled) {
        setPersonalError(
          `personal board: ${errMessage(err)} — the work board is unaffected`,
        );
      }
    });
    return () => {
      cancelled = true;
    };
  }, [personalAddr, login, config?.lockBoard, personalLoad, personalReset]);

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
  // The queries above need the roster but are declared before the hook that
  // yields the board it derives from; mirror it into state (stable-keyed, so
  // no refetch loops).
  useEffect(() => {
    setRosterForQueries((cur) =>
      cur.length === roster.length && cur.every((t, i) => t === roster[i])
        ? cur
        : roster,
    );
  }, [roster]);

  // A saved filter can outlive its teams (a team renamed away, or stale
  // pre-scoping data): entries no team backs would silently blank the board,
  // so prune them — and an emptied filter means "all" again. Gated on the
  // loaded board matching boardKeyRef: mid-switch the roster still reflects
  // the OLD board, and pruning the new board's filter against it would wipe
  // a legitimate saved selection.
  const loadedBoardKey = board ? `${board.owner}/${board.number}` : null;
  useEffect(() => {
    if (!loadedBoardKey || loadedBoardKey !== boardKeyRef.current) {
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
  }, [loadedBoardKey, roster]);

  // Reorder the whole roster (from the manage dialog) and persist the order.
  const reorderTeams = useCallback((ordered: string[]) => {
    setAddedTeams(ordered);
    writeStringList(scopedLS(LS_TEAM_ROSTER, boardKeyRef.current), ordered);
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
      writeStringList(scopedLS(LS_TEAM_ROSTER, boardKeyRef.current), next);
      return next;
    });
  }, []);

  const removeTeam = useCallback((team: string) => {
    setAddedTeams((cur) => {
      const next = cur.filter((t) => t !== team);
      writeStringList(scopedLS(LS_TEAM_ROSTER, boardKeyRef.current), next);
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

  // Persist the filter whenever it changes (null means "all", we store
  // nothing). boardKeyRef is updated synchronously by doLoad before the
  // swapped-in filter state lands, so the write always hits the right board.
  useEffect(() => {
    const key = scopedLS(LS_TEAM_FILTER, boardKeyRef.current);
    if (teamFilter === null) {
      localStorage.removeItem(key);
    } else {
      localStorage.setItem(key, JSON.stringify(teamFilter));
    }
  }, [teamFilter]);

  const doLoad = useCallback(
    async (ownerArg: string, numberArg: number) => {
      // Another board: swap in ITS saved roster and filter before anything
      // renders, so board 36's teams never leak into board 37's view.
      const boardKey = `${ownerArg}/${numberArg}`;
      if (boardKey !== boardKeyRef.current) {
        boardKeyRef.current = boardKey;
        setAddedTeams(readRosterFor(boardKey));
        setTeamFilter(readFilterFor(boardKey));
      }
      // The same project must never be both boards at once (two instances on
      // one project double every card and cross-route mutations). The dialog
      // refuses the work board; this covers the other direction — loading the
      // personal project AS the work board detaches the pointer, loudly.
      setPersonalPtr((ptr) => {
        if (ptr && samePointer(ptr, { owner: ownerArg, number: numberArg })) {
          if (login) {
            clearPersonalBoard(localStorage, login);
          }
          setPersonalTeamPersisted(false);
          setMePane("work");
          setDetail((cur) => (cur?.src === "personal" ? null : cur));
          setPersonalError(
            "The personal board was detached: you loaded the same project as the work board.",
          );
          return null;
        }
        return ptr;
      });
      setError(null);
      try {
        await work.load(ownerArg, numberArg);
      } catch (err: unknown) {
        setError(errMessage(err));
      }
    },
    [work],
  );

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

  // Fetch GitHub name + avatar for any assignee not looked up yet — across
  // BOTH boards. Watching them also covers people newly assigned during the
  // session.
  const personalBoard = personal.board;
  useEffect(() => {
    const todo: string[] = [];
    for (const c of [...(board?.cards ?? []), ...(personalBoard?.cards ?? [])]) {
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
  }, [board, personalBoard]);

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

  // Rename a team everywhere: the roster, the filter, and every card using it.
  const renameTeam = useCallback(
    (from: string, to: string) => {
      const t = to.trim();
      if (!t || t === from) {
        return;
      }
      setAddedTeams((cur) => {
        const next = [...new Set(cur.map((x) => (x === from ? t : x)))];
        writeStringList(scopedLS(LS_TEAM_ROSTER, boardKeyRef.current), next);
        return next;
      });
      setTeamFilter((cur) =>
        cur === null ? cur : cur.map((k) => (k === from ? t : k)),
      );
      work.setBoard((cur) => {
        if (!cur) {
          return cur;
        }
        for (const card of cur.cards) {
          if (card.team === from) {
            void provider
              .patchCard(cur, card.itemId, { team: t })
              .catch((err: unknown) => {
                workRef.current?.patchCard(card.itemId, { team: from });
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
    [provider, work],
  );

  // The pre-refactor reload went through doLoad and cleared the error banner;
  // keep that contract for the components' retry paths.
  const workReload = useCallback(() => {
    setError(null);
    work.reload();
  }, [work]);
  const personalReload = useCallback(() => {
    setPersonalError(null);
    personal.reload();
  }, [personal]);

  const showTokenWarning =
    config !== null && !config.tokenAvailable && !tokenWarningDismissed;

  const pendingSync = work.pendingSync + personal.pendingSync;

  // The single gate for everything personal (see personal.ts for why
  // lock-board is part of it: the server would pin the request to the work
  // project and silently serve it in the personal slot).
  const personalReady = personalPaneVisible({
    lockBoard: config?.lockBoard ?? false,
    viewAs,
    pointer: personalPtr,
    boardLoaded: personalBoard !== null,
  });
  const personalTeamActive = view === "team" && personalTeam && personalReady;

  const detailData = detail
    ? detail.src === "personal"
      ? personalBoard
        ? { board: personalBoard, api: personal }
        : null // never route a personal card's dialog at the work board
      : board
        ? { board, api: work }
        : null
    : null;

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

      {personalError && (
        <div className="banner banner-warning" role="alert">
          <span>{personalError}</span>
          <button
            type="button"
            className="banner-close"
            onClick={() => setPersonalError(null)}
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
        {login !== "" && !config?.lockBoard && (
          <button
            type="button"
            className={`btn personal-btn${personalPtr ? " personal-btn-attached" : ""}`}
            onClick={() => setPersonalOpen(true)}
            title={
              personalPtr
                ? `Personal board: ${personalPtr.owner}/${personalPtr.number}`
                : "Attach a personal board (visible only to you)"
            }
          >
            Personal{personalPtr ? "" : "…"}
          </button>
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
          <div
            className={`me-split${personalReady ? " me-split-dual" : ""}${
              mePane === "personal" ? " me-split-personal" : ""
            }`}
          >
            {personalReady && (
              <div className="pane-switch" role="tablist" aria-label="Board pane">
                <button
                  type="button"
                  className="day-arrow"
                  onClick={() => setMePane(prevPane)}
                  disabled={mePane === "work"}
                  aria-label="Show work tasks"
                >
                  ‹
                </button>
                <span className="pane-switch-title">
                  {mePane === "work" ? "Work" : "Personal"}
                </span>
                <button
                  type="button"
                  className="day-arrow"
                  onClick={() => setMePane(nextPane)}
                  disabled={mePane === "personal"}
                  aria-label="Show personal tasks"
                >
                  ›
                </button>
              </div>
            )}
            <div className="me-pane me-pane-work">
              {personalReady && <div className="pane-title">Work</div>}
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
                patchCard={work.patchCard}
                addCard={work.addCard}
                replaceCard={work.replaceCard}
                removeCard={work.removeCard}
                reorderCards={work.reorderCards}
                reload={workReload}
                onError={onError}
                onOpen={(c) => setDetail({ card: c, src: "work" })}
                onPresence={(card) => {
                  if (board && config?.login) {
                    void provider
                      .setPresence(board, config.login, card)
                      .catch(() => {});
                  }
                }}
              />
            </div>
            {personalReady && personalBoard && (
              <div className="me-pane me-pane-personal">
                <div className="pane-title">Personal</div>
                <MeBoard
                  board={personalBoard}
                  selectedDate={selectedDate}
                  onSelectDate={setSelectedDate}
                  viewAs={null}
                  onViewAs={() => {}}
                  provider={provider}
                  me={config?.login ?? ""}
                  users={users}
                  teams={[]}
                  teamFilter={null}
                  onSetFilter={() => {}}
                  onAddTeam={() => {}}
                  onRemoveTeam={() => {}}
                  onRenameTeam={() => {}}
                  patchCard={personal.patchCard}
                  addCard={personal.addCard}
                  replaceCard={personal.replaceCard}
                  removeCard={personal.removeCard}
                  reorderCards={personal.reorderCards}
                  reload={personalReload}
                  onError={onPersonalError}
                  onOpen={(c) => setDetail({ card: c, src: "personal" })}
                  embedded
                />
              </div>
            )}
          </div>
        )}
        {board && view === "team" && !personalTeamActive && (
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
            patchCard={work.patchCard}
            addCard={work.addCard}
            replaceCard={work.replaceCard}
            removeCard={work.removeCard}
            reorderCards={work.reorderCards}
            presence={work.presence}
            reload={workReload}
            track={trackLoad}
            onError={onError}
            onOpen={(c) => setDetail({ card: c, src: "work" })}
            personalChip={
              personalReady
                ? {
                    active: false,
                    onToggle: () => setPersonalTeamPersisted(true),
                  }
                : null
            }
          />
        )}
        {view === "team" && personalTeamActive && personalBoard && (
          <TeamBoard
            board={personalBoard}
            selectedDate={selectedDate}
            onSelectDate={setSelectedDate}
            provider={provider}
            me={config?.login ?? ""}
            users={users}
            roster={[]}
            teamFilter={[""]}
            onSetFilter={() => {}}
            onAddTeam={() => {}}
            onRemoveTeam={() => {}}
            onRenameTeam={() => {}}
            onReorderTeams={() => {}}
            patchCard={personal.patchCard}
            addCard={personal.addCard}
            replaceCard={personal.replaceCard}
            removeCard={personal.removeCard}
            reorderCards={personal.reorderCards}
            presence={personal.presence}
            reload={personalReload}
            track={trackLoad}
            onError={onPersonalError}
            onOpen={(c) => setDetail({ card: c, src: "personal" })}
            personalChip={{
              active: true,
              onToggle: () => setPersonalTeamPersisted(false),
            }}
          />
        )}
      </main>

      {detailData && detail && (
        <CardDetail
          card={
            detailData.board.cards.find((c) => c.itemId === detail.card.itemId) ??
            detail.card
          }
          board={detailData.board}
          provider={provider}
          onClose={() => setDetail(null)}
          reload={detail.src === "personal" ? personalReload : workReload}
          patchCard={detailData.api.patchCard}
        />
      )}

      {personalOpen && login && (
        <PersonalDialog
          login={login}
          current={personalPtr}
          workBoard={board ? { owner: board.owner, number: board.number } : null}
          onAttach={attachPersonal}
          onDetach={detachPersonal}
          onClose={() => setPersonalOpen(false)}
        />
      )}
    </div>
  );
}
