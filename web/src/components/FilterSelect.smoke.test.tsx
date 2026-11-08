import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import { closesOn } from "./Dropdown";
import { FilterSelect, FilterSelectMenu, filterLabel, toggled } from "./FilterSelect";

// THE ONE FILTER DROPDOWN every board's toolbar wears (Team, Customer,
// Owner): a button saying what is chosen, a menu of checkboxes —
// several are OR — and, where the board manages that roster, a "Manage…"
// entry at the bottom that opens the roster's dialog. The no-team group is
// an option like any team ("No team", the empty value).
describe("the filter dropdown", () => {
  it("says what is chosen, or that nothing narrows it", () => {
    const opts = [
      { value: "portal", label: "portal" },
      { value: "", label: "No team" },
    ];
    expect(filterLabel("Team", opts, [], "all")).toBe("Team: all");
    expect(filterLabel("Team", opts, ["portal", ""], "all")).toBe("Team: portal, No team");
    expect(filterLabel("Owner", [], ["ann"])).toBe("Owner: ann");
    const html = renderToStaticMarkup(<FilterSelect label="Team" options={opts} selected={[""]} onChange={vi.fn()} />);
    expect(html).toMatch(/aria-label="Team filter"[^>]*>Team: No team</);
    expect(html).toContain("filter-select-on");
  });

  it("ticks and unticks one value, keeping the rest", () => {
    expect(toggled(["a"], "b")).toEqual(["a", "b"]);
    expect(toggled(["a", "b"], "a")).toEqual(["b"]);
    expect(toggled(["a"], "")).toEqual(["a", ""]);
  });

  it("lists the options as checkboxes, a way to clear, and Manage at the bottom", () => {
    const html = renderToStaticMarkup(
      <FilterSelectMenu
        options={[
          { value: "portal", label: "portal" },
          { value: "", label: "No team" },
        ]}
        selected={["portal"]}
        anyLabel="all"
        onChange={vi.fn()}
        manage={{ label: "Manage teams", onClick: vi.fn() }}
        onClose={vi.fn()}
      />,
    );
    // A menu of checkable items, as the button's aria-haspopup="menu" says.
    expect(html.match(/role="menuitemcheckbox"/g)?.length).toBe(2);
    expect(html).toMatch(/role="menuitemcheckbox" aria-checked="true"[^>]*>.*portal/);
    expect(html).toMatch(/role="menuitemcheckbox" aria-checked="false"[^>]*>.*No team/);
    expect(html).toMatch(/<button[^>]*role="menuitem"[^>]*>Manage teams</);
    expect(html).toContain("No team");
    expect(html.indexOf("Manage teams")).toBeGreaterThan(html.indexOf("No team"));
    expect(html).toMatch(/<button[^>]*>All<\/button>/);
  });

  it("offers no Manage where the board manages nothing, and no clear with nothing chosen", () => {
    const html = renderToStaticMarkup(
      <FilterSelectMenu options={[{ value: "a", label: "a" }]} selected={[]} onChange={vi.fn()} onClose={vi.fn()} />,
    );
    expect(html).not.toContain("Manage");
    expect(html).not.toContain("card-stage-clear");
  });
});

// Escape closes an open menu and gives the focus back to what opened it.
describe("a dropdown and the keyboard", () => {
  it("closes on Escape and on nothing else", () => {
    expect(closesOn("Escape")).toBe(true);
    expect(closesOn("Enter")).toBe(false);
  });
});

// On a narrow screen a long choice is cut with an ellipsis, the button's
// title naming it in full, and the toolbar wraps instead of scrolling.
describe("the filter dropdown on a narrow screen", () => {
  it("cuts a long choice and names it in the title", () => {
    const opts = ["portal", "cozystack", "ops"].map((t) => ({ value: t, label: t }));
    const html = renderToStaticMarkup(<FilterSelect label="Team" options={opts} selected={["portal", "cozystack", "ops"]} onChange={vi.fn()} />);
    expect(html).toMatch(/title="Team: portal, cozystack, ops"/);
    const css = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");
    const btn = css.match(/\.filter-select-btn \{[^}]*\}/)?.[0] ?? "";
    expect(btn).toMatch(/max-width:/);
    expect(btn).toMatch(/text-overflow: ellipsis/);
    const bar = css.match(/\.board-toolbar \{[^}]*\}/)?.[0] ?? "";
    expect(bar).toMatch(/flex-wrap: wrap/);
  });
});

// The chip rows the dropdown replaced are gone, their styles with them; the
// toolbar's eye-shaped toggles (ViewToggle) stay.
describe("the styles of the chip rows", () => {
  it("are gone, the toggles' kept", () => {
    const css = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");
    expect(css).not.toMatch(/\.team-chips?\b|\.team-filter-chip|\.team-chip-toggle|\.team-select\b/);
    expect(css).toMatch(/\.team-eye \{/);
  });
});

describe("a quick choice in the filter dropdown", () => {
  // A QUICK choice sits at the foot of the menu where a board offers one,
  // drawn like the way back to "all" beside it — the Roadmap's Status
  // filter has "Hide done", one click for every status but done — checked
  // while the selection is exactly it.
  it("puts a board's quick choice at the foot, like All, checked while it holds", () => {
    const opts = [
      { value: "in-progress", label: "In progress" },
      { value: "backlog", label: "Backlog" },
      { value: "done", label: "Done" },
    ];
    const menu = (on: boolean) =>
      renderToStaticMarkup(
        <FilterSelectMenu
          options={opts}
          selected={on ? ["in-progress", "backlog"] : []}
          quick={{ label: "Hide done", on, onClick: vi.fn() }}
          onChange={vi.fn()}
          onClose={vi.fn()}
        />,
      );
    const off = menu(false);
    expect(off).toMatch(/role="menuitemcheckbox" aria-checked="false"[^>]*>.*Hide done/);
    expect(off.indexOf("Hide done")).toBeGreaterThan(off.indexOf("Done<"));
    expect(off).toMatch(/class="card-stage-item card-stage-clear filter-select-quick"[^>]*>.*Hide done/);
    // With a selection, the way back to all follows it, both at the foot.
    const on = menu(true);
    expect(on.indexOf("Hide done")).toBeGreaterThan(on.indexOf("Done<"));
    expect(on.indexOf("Hide done")).toBeLessThan(on.lastIndexOf("card-stage-clear\""));
    expect(menu(true)).toMatch(/role="menuitemcheckbox" aria-checked="true"[^>]*>.*Hide done/);
  });
});
