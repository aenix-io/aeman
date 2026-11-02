import { useRef } from "react";
import { Editor, defaultValueCtx, editorViewOptionsCtx, rootCtx } from "@milkdown/kit/core";
import { clipboard } from "@milkdown/kit/plugin/clipboard";
import { history } from "@milkdown/kit/plugin/history";
import { listener, listenerCtx } from "@milkdown/kit/plugin/listener";
import { commonmark, hardbreakSchema } from "@milkdown/kit/preset/commonmark";
import { gfm } from "@milkdown/kit/preset/gfm";
import { $remark } from "@milkdown/kit/utils";
import { Milkdown, MilkdownProvider, useEditor } from "@milkdown/react";
import remarkBreaks from "remark-breaks";
import "@milkdown/kit/prose/view/style/prosemirror.css";

// Line breaks the way a GitHub comment reads them: a single newline is a
// line break, not a space, so two lines typed one under the other stay two
// lines (remark-breaks on the way in)...
const breaks = $remark("breaks", () => remarkBreaks);

// ...and a line break is written back as a plain newline, so the saved text
// is the text the person typed — not "\" or two trailing spaces, which is
// what remark would otherwise write for a break and which no other reader
// of the file expects.
const hardbreakAsNewline = hardbreakSchema.extendSchema((prev) => (ctx) => ({
  ...prev(ctx),
  toMarkdown: {
    match: (node) => node.type.name === "hardbreak",
    runner: (state) => {
      state.addNode("text", undefined, "\n");
    },
  },
}));

export interface MarkdownEditorProps {
  /** The text as saved. The editor is remounted (keyed by the caller) when
   *  this changes under an untouched draft; while the user types, what
   *  they typed is the draft and this is not read again. */
  value: string;
  readOnly?: boolean;
  placeholder?: string;
  /** Every keystroke, as Markdown. */
  onChange: (markdown: string) => void;
  /** Ctrl/Cmd+Enter. */
  onSave: () => void;
  /** The editor lost the focus. */
  onBlur?: () => void;
}

/** The description editor: the text is read as Markdown and edited in
 *  place — a click anywhere in it puts the caret there, `**bold**` and the
 *  rest render as they are typed, and the clipboard carries Markdown both
 *  ways (clipboard plugin). Milkdown over ProseMirror, with Markdown as the
 *  source: what is saved is what `onChange` last said, never HTML. */
function MilkdownEditor({ value, readOnly, placeholder, onChange, onSave, onBlur }: MarkdownEditorProps) {
  const latest = useRef({ onChange, onBlur });
  latest.current = { onChange, onBlur };
  useEditor(
    (root) =>
      Editor.make()
        .config((ctx) => {
          ctx.set(rootCtx, root);
          ctx.set(defaultValueCtx, value);
          ctx.update(editorViewOptionsCtx, (prev) => ({
            ...prev,
            editable: () => !readOnly,
            attributes: { class: "md-editor-doc", "data-placeholder": placeholder ?? "" },
          }));
          const l = ctx.get(listenerCtx);
          l.markdownUpdated((_ctx, md, prev) => {
            if (md !== prev) {
              latest.current.onChange(md);
            }
          });
          l.blur(() => latest.current.onBlur?.());
        })
        .use(commonmark)
        .use(gfm)
        .use(breaks)
        .use(hardbreakAsNewline)
        .use(listener)
        .use(clipboard)
        .use(history),
    // The saved text and the read-only flag are the editor's own; a change
    // of either is a new editor (the caller keys the component on the card
    // and on the saved text for that reason).
    [readOnly],
  );
  // A click on a link places the caret in it, as in any editor; a double
  // click (or Cmd/Ctrl + click) FOLLOWS it, in a new tab, the way an editor
  // that renders its links is expected to. The link element is what the
  // pointer landed on, found before ProseMirror sees the event.
  const followLink = (e: React.MouseEvent<HTMLDivElement>) => {
    const a = (e.target as HTMLElement | null)?.closest?.("a[href]");
    const href = a?.getAttribute("href");
    if (!href) {
      return;
    }
    e.preventDefault();
    e.stopPropagation();
    window.open(href, "_blank", "noopener,noreferrer");
  };
  return (
    <div
      className={`md-editor${readOnly ? " md-editor-readonly" : ""}`}
      onKeyDownCapture={(e) => {
        if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
          e.preventDefault();
          onSave();
        }
      }}
      onDoubleClickCapture={followLink}
      onClickCapture={(e) => {
        if (e.metaKey || e.ctrlKey) {
          followLink(e);
        }
      }}
    >
      <Milkdown />
    </div>
  );
}

export default function MarkdownEditor(props: MarkdownEditorProps) {
  return (
    <MilkdownProvider>
      <MilkdownEditor {...props} />
    </MilkdownProvider>
  );
}
