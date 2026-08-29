import { describe, expect, it } from "vitest";

import { boardMetadata } from "./providers/api/apiProvider";
import type { BoardResource } from "./api/resources";

// A linked personal board the server cannot reach is a state the UI draws —
// the reason and a button — not an error met on the first write. The server
// says so in the board metadata and in refusals ({error, actionUrl}); the
// mapper must not drop it on the floor.
describe("a personal board that would not attach", () => {
  const resource = (personal: unknown): BoardResource =>
    ({ kind: "Board", metadata: { teams: [], members: [], personal } }) as BoardResource;

  it("carries the problem and the action through the mapper", () => {
    const meta = boardMetadata(
      resource({
        domain: "~kvaps",
        url: "https://github.com/kvaps/aeman-personal-db.git",
        problem: "authorization failed",
        actionUrl: "https://github.com/apps/aenix-aeman/installations/new",
      }),
    );
    expect(meta.personal?.problem).toBe("authorization failed");
    expect(meta.personal?.actionUrl).toBe(
      "https://github.com/apps/aenix-aeman/installations/new",
    );
  });

  it("leaves a healthy board without trouble fields", () => {
    const meta = boardMetadata(
      resource({ domain: "~kvaps", url: "https://github.com/kvaps/x.git" }),
    );
    expect(meta.personal?.problem).toBeUndefined();
    expect(meta.personal?.actionUrl).toBeUndefined();
  });
});
