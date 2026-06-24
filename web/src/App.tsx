import { useCallback, useEffect, useState } from "react";
import { fetchConfig, type AppConfig } from "./api/client";
import { getProvider } from "./providers";
import type { Board, Card as CardModel } from "./providers/types";
import { MeBoard } from "./components/MeBoard";
import { TeamBoard } from "./components/TeamBoard";
import { CardDetail } from "./components/CardDetail";

type ViewMode = "me" | "team";

const LS_OWNER = "aeman.owner";
const LS_PROJECT = "aeman.project";
const LS_VIEW = "aeman.view";

function readView(): ViewMode {
  const raw = localStorage.getItem(LS_VIEW);
  if (raw === "team" || raw === "nixon") {
    return "team";
  }
  return "me";
}

export function App() {
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [tokenWarningDismissed, setTokenWarningDismissed] = useState(false);

  const [owner, setOwner] = useState<string>(() => localStorage.getItem(LS_OWNER) ?? "");
  const [project, setProject] = useState<string>(
    () => localStorage.getItem(LS_PROJECT) ?? "",
  );
  const [view, setView] = useState<ViewMode>(readView);

  const [board, setBoard] = useState<Board | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [detailCard, setDetailCard] = useState<CardModel | null>(null);

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
          setError(err instanceof Error ? err.message : String(err));
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

  const doLoad = useCallback(
    async (ownerArg: string, numberArg: number) => {
      setLoading(true);
      setError(null);
      try {
        const loaded = await provider.loadBoard(ownerArg, numberArg);
        setBoard(loaded);
      } catch (err: unknown) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
      }
    },
    [provider],
  );

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

  const showTokenWarning =
    config !== null && !config.tokenAvailable && !tokenWarningDismissed;

  return (
    <div className="app">
      <header className="app-header">
        <div className="brand">
          <span className="wordmark">aeman</span>
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
            patchCard={patchCard}
            addCard={addCard}
            removeCard={removeCard}
            reorderCards={reorderCards}
            reload={reload}
            onError={onError}
            onOpen={(c) => setDetailCard(c)}
          />
        )}
        {board && view === "team" && (
          <TeamBoard
            board={board}
            provider={provider}
            me={config?.login ?? ""}
            patchCard={patchCard}
            addCard={addCard}
            removeCard={removeCard}
            reorderCards={reorderCards}
            reload={reload}
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
