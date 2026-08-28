// The forge is the identity provider and the remote the board lives on —
// GitHub or GitLab. The server names it in /api/config; every piece of copy
// that says "GitHub" somewhere in the UI is spelled from here so a GitLab
// board never tells its visitors to sign in with GitHub. An older server sends
// none of the fields: that is GitHub, exactly as before.

import type { AppConfig } from "./api/client";

export type ForgeKind = "github" | "gitlab";

export type ForgeConfig = Pick<AppConfig, "forge" | "forgeLabel" | "cli" | "forgeHost">;

export interface ForgeCopy {
  kind: ForgeKind;
  /** Human name of the forge: "GitHub", "GitLab", or whatever the server calls
   *  its self-hosted one. */
  label: string;
  /** The forge's CLI the local mode reads its token from: "gh" or "glab". */
  cli: string;
  /** The host repositories live on: "github.com", "gitlab.com", or the
   *  self-hosted instance. */
  host: string;
  signInTitle: string;
  signInLead: string;
  signInButton: string;
  /** The local-mode banner when the CLI holds no token. */
  noTokenHint: string;
  /** The personal-board dialog's URL placeholder. */
  repoPlaceholder: string;
  /** The MCP caption in the Connect dialog. */
  connectHint: string;
}

const DEFAULTS: Record<ForgeKind, { label: string; cli: string; host: string }> = {
  github: { label: "GitHub", cli: "gh", host: "github.com" },
  gitlab: { label: "GitLab", cli: "glab", host: "gitlab.com" },
};

/** forgeCopy spells the forge-specific copy from the server's config. Missing
 *  fields (and empty ones — a zero value on the wire) fall back to the kind's
 *  defaults; a missing kind is GitHub. */
export function forgeCopy(config: ForgeConfig | null | undefined): ForgeCopy {
  const kind: ForgeKind = config?.forge === "gitlab" ? "gitlab" : "github";
  const defaults = DEFAULTS[kind];
  const label = config?.forgeLabel || defaults.label;
  const cli = config?.cli || defaults.cli;
  const host = config?.forgeHost || defaults.host;
  return {
    kind,
    label,
    cli,
    host,
    signInTitle: "Sign in to aeman",
    signInLead: `Connect your ${label} account to open the board.`,
    signInButton: `Sign in with ${label}`,
    noTokenHint: `No ${label} token — run ${cli} auth login in the terminal where aeman runs.`,
    repoPlaceholder: `https://${host}/<you>/<repo>`,
    connectHint: `Sign in with ${label} on first use (OAuth, no token stored).`,
  };
}
