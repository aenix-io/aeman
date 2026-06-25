import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { fetchConfig, type AppConfig } from "./api/client";
import { getProvider } from "./providers";
import type { Board, Card as CardModel, Note } from "./providers/types";
import { MeBoard } from "./components/MeBoard";
import { TeamBoard } from "./components/TeamBoard";
import { CardDetail } from "./components/CardDetail";
import { LockDialog } from "./components/LockDialog";
import { Logo } from "./components/Logo";
import { fetchUsers, type GhUser } from "./users";

type ViewMode = "me" | "team";

const LS_OWNER = "aeman.owner";
const LS_PROJECT = "aeman.project";
const LS_VIEW = "aeman.view";
const LS_TEAM_ROSTER = "aeman.teamRoster";
const LS_TEAM_FILTER = "aeman.teamFilter";

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

export function App() {
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [tokenWarningDismissed, setTokenWarningDismissed] = useState(false);

  const [owner, setOwner] = useState<string>(() => localStorage.getItem(LS_OWNER) ?? "");
  const [project, setProject] = useState<string>(
    () => localStorage.getItem(LS_PROJECT) ?? "",
  );
  const [view, setView] = useState<ViewMode>(readView);

  const [board, setBoard] = useState<Board | null>(null);
  const [users, setUsers] = useState<Record<string, GhUser>>({});
  const fetchedUsers = useRef<Set<string>>(new Set());
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [detailCard, setDetailCard] = useState<CardModel | null>(null);
  const [lockCard, setLockCard] = useState<CardModel | null>(null);

  // Team roster + filter, persisted in localStorage. The roster is the union of
  // the teams found on the board and any teams the user has added by hand; the
  // filter is the subset of the roster currently shown (defaults to everything).
  const [addedTeams, setAddedTeams] = useState<string[]>(
    () => readStringList(LS_TEAM_ROSTER) ?? [],
  );
  // Single-select team filter: null = all, "" = no team, else a team name.
  const [teamFilter, setTeamFilter] = useState<string | null>(() => {
    const v = localStorage.getItem(LS_TEAM_FILTER);
    return v && !v.startsWith("[") ? v : null;
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

  const provider = getProvider("github");

  // The roster: teams present on the board ∪ user-added, deduplicated, sorted.
  const roster = useMemo(() => {
    const set = new Set<string>(addedTeams);
    if (board) {
      for (const card of board.cards) {
        if (card.team) {
          set.add(card.team);
        }
      }
    }
    return [...set].sort((a, b) => a.localeCompare(b));
  }, [addedTeams, board]);

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
    // Clear the filter if it pointed at the removed team.
    setTeamFilter((cur) => (cur === team ? null : cur));
  }, []);

  // Persist the filter whenever it changes (null means "all", we store nothing).
  useEffect(() => {
    if (teamFilter === null) {
      localStorage.removeItem(LS_TEAM_FILTER);
    } else {
      localStorage.setItem(LS_TEAM_FILTER, teamFilter);
    }
  }, [teamFilter]);

  const doLoad = useCallback(
    async (ownerArg: string, numberArg: number) => {
      setLoading(true);
      setError(null);
      try {
        const loaded = await provider.loadBoard(ownerArg, numberArg);
        setBoard(loaded);
      } catch (err: unknown) {
        setError(errMessage(err));
      } finally {
        setLoading(false);
      }
    },
    [provider],
  );

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

  const patchCard = useCallback((itemId: string, patch: Partial<CardModel>) => {
    setBoard((cur) => {
      if (!cur) {
        return cur;
      }
      return {
        ...cur,
        cards: cur.cards.map((c) => (c.itemId === itemId ? { ...c, ...patch } : c)),
      };
    });
  }, []);

  const addCard = useCallback((card: CardModel) => {
    setBoard((cur) => (cur ? { ...cur, cards: [...cur.cards, card] } : cur));
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
      setTeamFilter((cur) => (cur === from ? t : cur));
      setBoard((cur) => {
        if (!cur) {
          return cur;
        }
        for (const card of cur.cards) {
          if (card.team === from) {
            void provider.setTeam(cur, card, t).catch((err: unknown) => {
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

  // Locking posts a note (the reason) to the card so it shows in the day's log.
  const handleLock = useCallback(
    (card: CardModel, note: string) => {
      if (!board) {
        return;
      }
      const prevStage = card.stage;
      const optimisticNote: Note = {
        id: `tmp-${new Date().toISOString()}`,
        body: note,
        createdAt: new Date().toISOString(),
        author: config?.login || undefined,
        source: card.isDraft ? "draft" : "comment",
      };
      patchCard(card.itemId, {
        stage: "locked",
        notes: [...(card.notes ?? []), optimisticNote],
      });
      void (async () => {
        try {
          await provider.setStage(board, card, "locked");
          await provider.addNote(board, card, note);
        } catch (err: unknown) {
          patchCard(card.itemId, { stage: prevStage });
          setError(errMessage(err));
          reload();
        }
      })();
    },
    [board, config?.login, patchCard, provider, reload],
  );

  const showTokenWarning =
    config !== null && !config.tokenAvailable && !tokenWarningDismissed;

  return (
    <div className="app">
      <header className="app-header">
        <div className="brand">
          <Logo className="brand-logo" />
          {config && <span className="version">v{config.version}</span>}
        </div>
        <div className="account">
          {config?.login ? (
            <span className="login">@{config.login}</span>
          ) : (
            <span className="login login-anon">not signed in</span>
          )}
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
        <button type="button" className="btn btn-primary" onClick={handleLoad} disabled={loading}>
          {loading ? "Loading…" : "Load"}
        </button>

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
          <p className="placeholder">Enter an owner and project number, then press Load.</p>
        )}
        {board && view === "me" && (
          <MeBoard
            board={board}
            provider={provider}
            me={config?.login ?? ""}
            teams={roster}
            onAddTeam={addTeam}
            onRemoveTeam={removeTeam}
            onRenameTeam={renameTeam}
            patchCard={patchCard}
            addCard={addCard}
            removeCard={removeCard}
            reorderCards={reorderCards}
            reload={reload}
            onError={onError}
            onOpen={(c) => setDetailCard(c)}
            onRequestLock={(c) => setLockCard(c)}
          />
        )}
        {board && view === "team" && (
          <TeamBoard
            board={board}
            provider={provider}
            me={config?.login ?? ""}
            users={users}
            roster={roster}
            teamFilter={teamFilter}
            onSetFilter={setTeamFilter}
            onAddTeam={addTeam}
            onRemoveTeam={removeTeam}
            onRenameTeam={renameTeam}
            patchCard={patchCard}
            addCard={addCard}
            removeCard={removeCard}
            reorderCards={reorderCards}
            reload={reload}
            onError={onError}
            onOpen={(c) => setDetailCard(c)}
            onRequestLock={(c) => setLockCard(c)}
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

      {board && lockCard && (
        <LockDialog
          card={board.cards.find((c) => c.itemId === lockCard.itemId) ?? lockCard}
          onClose={() => setLockCard(null)}
          onSubmit={handleLock}
        />
      )}
    </div>
  );
}
