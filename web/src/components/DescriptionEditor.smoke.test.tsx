import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { DescriptionEditor, PlainEditor } from "./DescriptionEditor";

// The description is edited in a Markdown editor that is a chunk of its own,
// loaded when a pane first opens; until it is there — and if it never comes
// — the text is in a plain box with the same draft and the same save keys.
// There is no browser in this suite, so what is pinned is that contract: a
// server render (no chunk, no document) is the box, with the draft in it,
// disabled while the body is still being fetched, and the editor module
// itself is importable and default-exports a component.

describe("the description editor", () => {
  it("falls back to a plain box carrying the draft while the editor is not there", () => {
    const html = renderToStaticMarkup(
      <DescriptionEditor draft="Hello **world**" value="Hello **world**" onChange={vi.fn()} onSave={vi.fn()} />,
    );
    expect(html).toContain("<textarea");
    expect(html).toContain("Hello **world**");
    expect(html).not.toContain("disabled");
  });

  it("disables the box while the body is still being fetched, and never mounts the editor on nothing", () => {
    const html = renderToStaticMarkup(
      <DescriptionEditor draft="" value="" pending placeholder="Loading the description…" onChange={vi.fn()} onSave={vi.fn()} />,
    );
    expect(html).toContain("disabled");
    expect(html).toContain("Loading the description…");
  });

  it("reads only on a record", () => {
    const html = renderToStaticMarkup(<PlainEditor value="x" readOnly onChange={vi.fn()} onSave={vi.fn()} />);
    expect(html).toMatch(/read-?only/i);
  });

  it("ships the Markdown editor as a component of its own", async () => {
    const mod = await import("./MarkdownEditor");
    expect(typeof mod.default).toBe("function");
  });
});
