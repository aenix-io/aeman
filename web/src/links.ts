/** Link extraction from a card's description — the client mirror of
 * board.ExtractLinks on the server. The local pass drives the links icon and
 * the instant (unresolved) menu; GET /cards/{uid}/links resolves GitHub
 * issue/PR references to their titles when the menu opens. */

export interface CardLink {
  url: string;
  /** "issue" | "pull" for GitHub references, "link" for everything else. */
  kind: "issue" | "pull" | "link";
  owner?: string;
  repo?: string;
  number?: number;
  /** Issue/PR title, when the server resolved it. */
  title?: string;
  /** Issue/PR state (open/closed/merged/draft), when resolved. */
  state?: string;
}

const URL_PATTERN = /https?:\/\/[^\s<>"'\)\]]+/g;

/** extractLinks finds every URL in a description, classifies GitHub issue/PR
 * links, dedupes, and orders GitHub references first, plain links after. */
export function extractLinks(description: string): CardLink[] {
  const refs: CardLink[] = [];
  const plain: CardLink[] = [];
  const seen = new Set<string>();
  for (const raw of description.match(URL_PATTERN) ?? []) {
    const url = raw.replace(/[.,;:!?]+$/, "");
    if (seen.has(url)) {
      continue;
    }
    seen.add(url);
    const link = classifyLink(url);
    (link.kind === "link" ? plain : refs).push(link);
  }
  return [...refs, ...plain];
}

/** classifyLink recognises github.com/{owner}/{repo}/issues|pull/{n}. */
function classifyLink(url: string): CardLink {
  const link: CardLink = { url, kind: "link" };
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return link;
  }
  const host = parsed.hostname.toLowerCase().replace(/^www\./, "");
  if (host !== "github.com") {
    return link;
  }
  const parts = parsed.pathname.replace(/^\/+|\/+$/g, "").split("/");
  if (parts.length < 4) {
    return link;
  }
  const n = Number(parts[3]);
  if (!Number.isInteger(n) || n <= 0) {
    return link;
  }
  if (parts[2] === "issues") {
    return { url, kind: "issue", owner: parts[0], repo: parts[1], number: n };
  }
  if (parts[2] === "pull") {
    return { url, kind: "pull", owner: parts[0], repo: parts[1], number: n };
  }
  return link;
}
