import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { OwnerBadge } from "./OwnerBadge";

// Whose a task is, drawn the same wherever an owner is shown (the Strategy board's
// Owner column, the Backlog card): the TEAM's avatar as the Me and Team
// boards draw it; with a person, the person's avatar to its right,
// OVERLAPPING it by a quarter of its width, on top; and beside the group one label —
// the team when nobody has it, the person when somebody does. No dot, no
// "team · person" text.
describe("the owner badge", () => {
  it("draws the team's avatar and names the team when nobody has the task", () => {
    const html = renderToStaticMarkup(<OwnerBadge owner={{ team: "portal", person: "" }} />);
    expect(html).toContain('class="team-avatar owner-badge-team"');
    expect(html).toContain('title="Team portal"');
    expect(html).not.toContain("owner-badge-person");
    expect(html).toContain('<span class="owner-badge-label">portal</span>');
    expect(html).not.toContain("team-dot");
    expect(html).not.toContain("·");
  });

  it("stacks the person's avatar over the team's, overlapping, and names the person", () => {
    const html = renderToStaticMarkup(
      <OwnerBadge owner={{ team: "portal", person: "kvaps" }} names={{ kvaps: "Andrei Kvapil" }} />,
    );
    expect(html).toContain("owner-badge-stack owner-badge-overlap");
    expect(html.indexOf("owner-badge-team")).toBeLessThan(html.indexOf("owner-badge-person"));
    expect(html).toContain('<span class="owner-badge-label">Andrei Kvapil</span>');
  });

  // The Backlog card draws the avatars alone: the team's name is the
  // avatar's tooltip, and there is no label beside them.
  it("can be drawn without its label", () => {
    const html = renderToStaticMarkup(<OwnerBadge owner={{ team: "portal", person: "" }} label={false} />);
    expect(html).toContain('title="Team portal"');
    expect(html).not.toContain("owner-badge-label");
  });

  // The person's avatar overlaps the team's by a QUARTER of its width.
  it("overlaps the person over the team by a quarter of the avatar", () => {
    const css = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");
    expect(css).toMatch(/\.owner-badge-overlap \.owner-badge-person \{\s*margin-left: -4px;/);
  });

  it("names the no-team group when the task has no team", () => {
    const html = renderToStaticMarkup(<OwnerBadge owner={{ team: "", person: "" }} />);
    expect(html).toContain("team-avatar-empty");
    expect(html).toContain('<span class="owner-badge-label">No team</span>');
  });
});
