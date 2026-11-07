import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ConnectDialog } from "./ConnectDialog";

// How a person reaches the board from their tools: Claude Code by one
// command, ChatGPT by the endpoint pasted into a custom connector (it speaks
// the stateless protocol, G86), the REST API by its base URL.
describe("the connect dialog", () => {
  beforeEach(() => {
    vi.stubGlobal("window", { location: { origin: "https://aeman.example.org" } });
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const draw = () => renderToStaticMarkup(<ConnectDialog onClose={vi.fn()} connectHint="Sign in with GitHub on first use." />);

  it("gives Claude Code its one command", () => {
    expect(draw()).toContain("claude mcp add --transport http aeman https://aeman.example.org/mcp");
  });

  it("gives ChatGPT the MCP endpoint to paste into a connector, with OAuth", () => {
    const html = draw();
    expect(html).toContain("MCP — for ChatGPT");
    expect(html).toMatch(/<code>https:\/\/aeman\.example\.org\/mcp<\/code>/);
    expect(html).toContain("OAuth");
  });

  it("copies each block on its own", () => {
    const html = draw();
    expect(html.match(/class="connect-copy"/g)?.length).toBe(3);
  });
});
