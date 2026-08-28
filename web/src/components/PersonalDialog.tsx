import { useState } from "react";

interface PersonalDialogProps {
  onClose: () => void;
  /** Link the repository; rejects with the server's message (no push access,
   *  a bad URL, a repository that cannot be reached). */
  onLink: (url: string) => Promise<void>;
}

const errMessage = (err: unknown) =>
  err instanceof Error ? err.message : String(err);

/** PersonalDialog asks for the repository to link as the visitor's personal
 *  board: one URL, one button. It stays open on failure with the server's
 *  reason under the field, and closes itself once the link is made. */
export function PersonalDialog({ onClose, onLink }: PersonalDialogProps) {
  const [url, setUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = () => {
    const u = url.trim();
    if (!u || busy) {
      return;
    }
    setBusy(true);
    setError(null);
    onLink(u)
      .then(onClose)
      .catch((err: unknown) => {
        setError(errMessage(err));
        setBusy(false);
      });
  };

  return (
    <div className="modal-backdrop" onClick={onClose} role="presentation">
      <div
        className="modal modal-narrow"
        role="dialog"
        aria-modal="true"
        aria-label="Personal board"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="modal-header">
          <h2 className="modal-title">Personal board</h2>
          <button
            type="button"
            className="modal-close"
            onClick={onClose}
            aria-label="Close"
          >
            ✕
          </button>
        </div>

        <div className="modal-body">
          <p className="personal-hint">
            A repository of your own, shown as a column on your Me board. You need
            push access to it: aeman commits there with your credential, and nobody
            else on the board sees it.
          </p>
          <label className="modal-field">
            <span>Repository URL</span>
            <input
              type="url"
              className="add-card-input"
              autoFocus
              value={url}
              placeholder="https://github.com/<you>/<repo>"
              disabled={busy}
              onChange={(e) => setUrl(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  submit();
                } else if (e.key === "Escape") {
                  onClose();
                }
              }}
            />
          </label>
          {error && (
            <p className="personal-error" role="alert">
              {error}
            </p>
          )}
        </div>

        <div className="modal-footer">
          <button type="button" className="btn" onClick={onClose}>
            Cancel
          </button>
          <button
            type="button"
            className="btn btn-primary"
            disabled={!url.trim() || busy}
            onClick={submit}
          >
            {busy ? "Linking…" : "Link"}
          </button>
        </div>
      </div>
    </div>
  );
}
