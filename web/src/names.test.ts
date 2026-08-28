import { describe, expect, it } from "vitest";
import { nameConflict } from "./names";

describe("nameConflict", () => {
  const teams = ["portal", "Features", " sales "];

  it("names the entry that already carries the name", () => {
    expect(nameConflict("team", teams, "portal")).toBe(
      "A team named “portal” already exists",
    );
  });

  it("compares case-insensitively and trimmed, and reports the existing spelling", () => {
    expect(nameConflict("team", teams, "PORTAL")).toBe(
      "A team named “portal” already exists",
    );
    expect(nameConflict("team", teams, "features")).toBe(
      "A team named “Features” already exists",
    );
    expect(nameConflict("team", teams, "  sales")).toBe(
      "A team named “ sales ” already exists",
    );
  });

  it("lets a rename keep its own name, in any case", () => {
    expect(nameConflict("team", teams, "portal", "portal")).toBeNull();
    expect(nameConflict("team", teams, "Portal", "portal")).toBeNull();
  });

  it("still refuses a rename onto another entry", () => {
    expect(nameConflict("project", ["freedom", "events"], "events", "freedom")).toBe(
      "A project named “events” already exists",
    );
  });

  it("picks the article for the kind", () => {
    expect(nameConflict("epic", ["Auth"], "auth")).toBe(
      "An epic named “Auth” already exists",
    );
  });

  it("has nothing to say about a free or an empty name", () => {
    expect(nameConflict("process", ["Invoicing"], "Runway")).toBeNull();
    expect(nameConflict("process", ["Invoicing"], "   ")).toBeNull();
    expect(nameConflict("team", [], "anything")).toBeNull();
  });
});
