import { describe, expect, it } from "vitest";

import { forgeCopy } from "./forge";

const github = {
  kind: "github",
  label: "GitHub",
  cli: "gh",
  host: "github.com",
  signInTitle: "Sign in to aeman",
  signInLead: "Connect your GitHub account to open the board.",
  signInButton: "Sign in with GitHub",
  noTokenHint: "No GitHub token — run aeman login, or gh auth login, in the terminal where aeman runs.",
  repoPlaceholder: "https://github.com/<you>/<repo>",
  connectHint: "Sign in with GitHub on first use (OAuth, no token stored).",
};

const gitlab = {
  kind: "gitlab",
  label: "GitLab",
  cli: "glab",
  host: "gitlab.com",
  signInTitle: "Sign in to aeman",
  signInLead: "Connect your GitLab account to open the board.",
  signInButton: "Sign in with GitLab",
  noTokenHint: "No GitLab token — run aeman login, or glab auth login, in the terminal where aeman runs.",
  repoPlaceholder: "https://gitlab.com/<you>/<repo>",
  connectHint: "Sign in with GitLab on first use (OAuth, no token stored).",
};

describe("forgeCopy", () => {
  it("is GitHub when there is no config yet (the sign-in gate renders before /api/config answers)", () => {
    expect(forgeCopy(undefined)).toEqual(github);
    expect(forgeCopy(null)).toEqual(github);
  });

  it("is GitHub when an older server names no forge at all", () => {
    expect(forgeCopy({})).toEqual(github);
  });

  it("spells the GitHub copy from an explicit GitHub config", () => {
    expect(
      forgeCopy({ forge: "github", forgeLabel: "GitHub", cli: "gh", forgeHost: "github.com" }),
    ).toEqual(github);
  });

  it("spells the GitLab copy from a gitlab.com config", () => {
    expect(
      forgeCopy({ forge: "gitlab", forgeLabel: "GitLab", cli: "glab", forgeHost: "gitlab.com" }),
    ).toEqual(gitlab);
  });

  it("fills the GitLab label, CLI and host in when the server names only the kind", () => {
    expect(forgeCopy({ forge: "gitlab" })).toEqual(gitlab);
  });

  it("puts a self-hosted host into the repository placeholder and keeps the rest", () => {
    const copy = forgeCopy({ forge: "gitlab", forgeHost: "git.example.com" });
    expect(copy.host).toBe("git.example.com");
    expect(copy.repoPlaceholder).toBe("https://git.example.com/<you>/<repo>");
    expect(copy.signInButton).toBe("Sign in with GitLab");
    expect(copy.noTokenHint).toBe(gitlab.noTokenHint);
  });

  it("lets the server's label and CLI win over the kind's defaults (an enterprise GitHub)", () => {
    const copy = forgeCopy({
      forge: "github",
      forgeLabel: "GitHub Enterprise",
      cli: "gh",
      forgeHost: "github.example.com",
    });
    expect(copy.label).toBe("GitHub Enterprise");
    expect(copy.signInLead).toBe("Connect your GitHub Enterprise account to open the board.");
    expect(copy.signInButton).toBe("Sign in with GitHub Enterprise");
    expect(copy.repoPlaceholder).toBe("https://github.example.com/<you>/<repo>");
    expect(copy.connectHint).toBe(
      "Sign in with GitHub Enterprise on first use (OAuth, no token stored).",
    );
  });

  it("treats empty strings as absent, so a zero-valued field falls back to the kind's default", () => {
    expect(forgeCopy({ forge: "gitlab", forgeLabel: "", cli: "", forgeHost: "" })).toEqual(gitlab);
  });

  it("keeps the sign-in title forge-neutral", () => {
    expect(forgeCopy({ forge: "gitlab" }).signInTitle).toBe(github.signInTitle);
  });
});
