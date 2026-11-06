import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import { FilterSelect, FilterSelectMenu, filterLabel, toggled } from "./FilterSelect";

// THE ONE FILTER DROPDOWN every board's toolbar wears (Team, Customer,
// Owner, Project): a button saying what is chosen, a menu of checkboxes —
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
    expect(html.match(/type="checkbox"/g)?.length).toBe(2);
    expect(html).toMatch(/checked=""[^>]*\/>portal|portal/);
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
