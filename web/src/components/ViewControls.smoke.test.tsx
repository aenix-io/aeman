import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { BacklogModeSwitch, StrategyControls } from "./ViewControls";

// The Strategy board's Hierarchy range and zoom, and the Backlog's Tasks |
// Epics, stand in the app's top bar (each on its own tab); the boards'
// toolbars keep the filters.
describe("the board controls of the top bar", () => {
  it("draws the Hierarchy range and the zoom, the chosen one pressed", () => {
    const html = renderToStaticMarkup(
      <StrategyControls depth={{ from: "initiative", to: "task" }} onDepth={vi.fn()} zoom="1y" onZoom={vi.fn()} />,
    );
    expect(html).toContain("Hierarchy");
    expect(html).toContain('aria-label="From level"');
    expect(html).toMatch(/segment segment-active"[^>]*>1Y</);
  });

  it("draws the Hierarchy range and the window it shows at the left, the zoom apart in the middle", () => {
    const html = renderToStaticMarkup(
      <StrategyControls depth={{ from: "initiative", to: "task" }} onDepth={vi.fn()} zoom="3m" onZoom={vi.fn()} range="19 Oct – 18 Jan 2027" />,
    );
    const at = (t: string) => html.indexOf(t);
    expect(at("Hierarchy")).toBeLessThan(at("19 Oct – 18 Jan 2027"));
    expect(html).toContain('class="topbar-controls"');
    // The zoom is its own block, centred in the bar, not in the left group.
    expect(html).toMatch(/class="topbar-center"[^>]*>.*>3M</s);
    const left = html.slice(html.indexOf('class="topbar-controls"'), html.indexOf('class="topbar-center"'));
    expect(left).not.toContain(">3M<");
  });

  it("draws Tasks | Epics, the chosen one pressed", () => {
    const html = renderToStaticMarkup(<BacklogModeSwitch mode="epics" onMode={vi.fn()} />);
    expect(html).toMatch(/aria-pressed="true"[^>]*>Epics</);
    expect(html).toMatch(/aria-pressed="false"[^>]*>Tasks</);
  });
});
