import { useRef } from "react";
import { Editor, defaultValueCtx, editorViewOptionsCtx, rootCtx } from "@milkdown/kit/core";
import { clipboard } from "@milkdown/kit/plugin/clipboard";
import { history } from "@milkdown/kit/plugin/history";
import { listener, listenerCtx } from "@milkdown/kit/plugin/listener";
import { commonmark } from "@milkdown/kit/preset/commonmark";
import { gfm } from "@milkdown/kit/preset/gfm";
import { Milkdown, MilkdownProvider, useEditor } from "@milkdown/react";
import "@milkdown/kit/prose/view/style/prosemirror.css";

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
        .use(listener)
        .use(clipboard)
        .use(history),
    // The saved text and the read-only flag are the editor's own; a change
    // of either is a new editor (the caller keys the component on the card
    // and on the saved text for that reason).
    [readOnly],
  );
  return (
    <div
      className={`md-editor${readOnly ? " md-editor-readonly" : ""}`}
      onKeyDownCapture={(e) => {
        if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
          e.preventDefault();
          onSave();
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
