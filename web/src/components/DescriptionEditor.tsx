import { Component, Suspense, lazy, type ReactNode } from "react";
import type { MarkdownEditorProps } from "./MarkdownEditor";

// The description, as the drawer edits it: the Markdown editor when it
// loads, a plain box when it cannot. The editor is a chunk of its own —
// ProseMirror and the Markdown presets weigh more than the rest of the
// board — fetched the first time a pane opens, and a failure to fetch it
// (an old tab after a deploy, a flaky line) must not take the description
// with it: the text is still readable and editable in the box.

const MarkdownEditor = lazy(() => import("./MarkdownEditor"));

interface FallbackProps extends MarkdownEditorProps {
  disabled?: boolean;
}

/** PlainEditor is the box the description is edited in when the Markdown
 *  editor is not there: the same text, the same save keys. */
export function PlainEditor({ value, readOnly, disabled, placeholder, onChange, onSave, onBlur }: FallbackProps) {
  return (
    <textarea
      className="modal-textarea drawer-textarea"
      value={value}
      placeholder={placeholder}
      maxLength={16384}
      readOnly={readOnly}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
      onBlur={onBlur}
      onKeyDown={(e) => {
        if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
          e.preventDefault();
          onSave();
        }
      }}
    />
  );
}

interface BoundaryProps {
  fallback: ReactNode;
  children: ReactNode;
}

class EditorBoundary extends Component<BoundaryProps, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  render() {
    return this.state.failed ? this.props.fallback : this.props.children;
  }
}

interface DescriptionEditorProps extends FallbackProps {
  /** The draft the fallback box shows while the editor loads, and what it
   *  edits if the editor never comes. */
  draft: string;
  /** Not loaded yet (the body is still being fetched): the box is disabled
   *  and the editor is not mounted, since it would open on "". */
  pending?: boolean;
}

/** DescriptionEditor is the description as the drawer shows it. */
export function DescriptionEditor({ draft, pending, disabled, ...props }: DescriptionEditorProps) {
  const plain = <PlainEditor {...props} value={draft} disabled={disabled || pending} />;
  if (pending) {
    return plain;
  }
  return (
    <EditorBoundary fallback={plain}>
      <Suspense fallback={plain}>
        <MarkdownEditor {...props} />
      </Suspense>
    </EditorBoundary>
  );
}
