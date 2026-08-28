import { useState } from "react";

interface ConnectDialogProps {
  onClose: () => void;
  /** The MCP caption: how the first call signs in, spelled for the board's
   *  forge (see forgeCopy). */
  connectHint: string;
}

/**
 * ConnectDialog shows how to reach aeman programmatically: the MCP endpoint for
 * Claude Code / agents, and the REST API base URL with its catalog.
 *
 * The "copied" feedback is timer-free: clicking a copy button records which
 * block was copied in local state, and the label reads "copied" until another
 * block is copied or the dialog is reopened (the component unmounts on close,
 * so the flag resets naturally — no setTimeout to leak).
 */
export function ConnectDialog({ onClose, connectHint }: ConnectDialogProps) {
  const origin = window.location.origin;
  const mcpCmd = `claude mcp add --transport http aeman ${origin}/mcp`;
  const apiUrl = `${origin}/api/v1`;
  const [copied, setCopied] = useState<string | null>(null);

  const copy = (key: string, text: string) => {
    void navigator.clipboard?.writeText(text);
    setCopied(key);
  };

  return (
    <div
      className="modal-backdrop"
      onClick={onClose}
      role="presentation"
    >
      <div
        className="modal connect-dialog"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="Connect to aeman"
      >
        <div className="modal-header">
          <h2 className="modal-title">Connect to aeman</h2>
          <button
            type="button"
            className="modal-close"
            onClick={onClose}
            aria-label="Close"
          >
            ×
          </button>
        </div>
        <div className="modal-body">
          <section className="connect-section">
            <h3 className="connect-heading">MCP — for Claude Code / agents</h3>
            <div className="connect-row">
              <pre className="connect-cmd">
                <code>{mcpCmd}</code>
              </pre>
              <button
                type="button"
                className="connect-copy"
                onClick={() => copy("mcp", mcpCmd)}
              >
                {copied === "mcp" ? "copied" : "copy"}
              </button>
            </div>
            <p className="connect-caption">{connectHint}</p>
          </section>

          <section className="connect-section">
            <h3 className="connect-heading">REST API</h3>
            <div className="connect-row">
              <pre className="connect-cmd">
                <a href={apiUrl} target="_blank" rel="noreferrer">
                  {apiUrl}
                </a>
              </pre>
              <button
                type="button"
                className="connect-copy"
                onClick={() => copy("api", apiUrl)}
              >
                {copied === "api" ? "copied" : "copy"}
              </button>
            </div>
            <p className="connect-caption">
              Same board operations over HTTP — open the link for the endpoint
              catalog.
            </p>
          </section>
        </div>
      </div>
    </div>
  );
}
