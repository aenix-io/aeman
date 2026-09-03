import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
  type RefObject,
} from "react";
import { createPortal } from "react-dom";

interface DropdownProps {
  open: boolean;
  /** The element the menu is anchored to (its trigger / wrapper). */
  anchorRef: RefObject<HTMLElement | null>;
  onClose: () => void;
  className?: string;
  children: ReactNode;
}

/**
 * Dropdown renders a menu in a body portal so it is never clipped by a
 * scrolling/overflow ancestor. It positions the menu under the anchor and
 * flips it above when it would overflow the bottom of the viewport.
 */
export function Dropdown({ open, anchorRef, onClose, className, children }: DropdownProps) {
  const menuRef = useRef<HTMLDivElement | null>(null);
  const [style, setStyle] = useState<CSSProperties>({
    position: "fixed",
    top: 0,
    left: 0,
    right: "auto",
    // Above everything it can be opened over — the card modal's backdrop
    // included, which is 100. A menu portalled to <body> with no layer of
    // its own stacks in document order and opened out of sight behind it.
    zIndex: 200,
    visibility: "hidden",
  });

  const place = useCallback(() => {
    const anchor = anchorRef.current;
    const menu = menuRef.current;
    if (!anchor || !menu) {
      return;
    }
    const a = anchor.getBoundingClientRect();
    const m = menu.getBoundingClientRect();
    const gap = 4;
    // Right-aligned to the anchor where there is room, but CLAMPED to the
    // viewport: a narrow anchor near the left edge (the week column) would
    // otherwise push the menu off-screen to the left.
    const left = Math.min(
      Math.max(8, a.right - m.width),
      Math.max(8, window.innerWidth - m.width - 8),
    );
    let top = a.bottom + gap;
    if (top + m.height > window.innerHeight - 8) {
      top = Math.max(8, a.top - gap - m.height);
    }
    setStyle({
      position: "fixed",
      top,
      left,
      right: "auto",
      visibility: "visible",
      // The same layer the hidden state starts on: this call writes the
      // style afresh, so a lower one here puts the menu back under the card
      // modal it was opened over.
      zIndex: 200,
    });
  }, [anchorRef]);

  useLayoutEffect(() => {
    if (open) {
      place();
    }
  }, [open, place]);

  useEffect(() => {
    if (!open) {
      return;
    }
    const reposition = () => place();
    // pointerdown, not mousedown: anything that begins a drag calls
    // preventDefault() on the pointer event, and that suppresses the
    // compatibility mouse events entirely — so a menu opened over the board
    // would never hear the press that was meant to dismiss it.
    //
    // And in the CAPTURE phase, because the same drag handlers call
    // stopPropagation(): a press on a board cell never reached a listener
    // waiting on the document, so clicking away from an open menu left it
    // open. Capture runs on the way DOWN, where nothing can stop it.
    const onDown = (e: PointerEvent) => {
      const t = e.target as Node;
      if (menuRef.current?.contains(t) || anchorRef.current?.contains(t)) {
        return;
      }
      onClose();
    };
    window.addEventListener("scroll", reposition, true);
    window.addEventListener("resize", reposition);
    document.addEventListener("pointerdown", onDown, true);
    return () => {
      window.removeEventListener("scroll", reposition, true);
      window.removeEventListener("resize", reposition);
      document.removeEventListener("pointerdown", onDown, true);
    };
  }, [open, place, onClose, anchorRef]);

  if (!open) {
    return null;
  }
  return createPortal(
    <div ref={menuRef} className={className} style={style}>
      {children}
    </div>,
    document.body,
  );
}
