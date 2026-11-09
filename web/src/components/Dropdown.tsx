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

import { anchorShown, placePopup } from "../popup";

interface DropdownProps {
  open: boolean;
  /** The element the menu is anchored to (its trigger / wrapper). */
  anchorRef: RefObject<HTMLElement | null>;
  onClose: () => void;
  className?: string;
  /** The menu's role, matching the trigger's aria-haspopup. */
  role?: string;
  /** "start" opens the menu from the pressed control's left edge — a
   *  picker whose control sits at the start of a wide row; "end" (the
   *  default) lines it up with the control's right edge. */
  align?: "start" | "end";
  children: ReactNode;
}

/** triggerOf is the control a menu belongs to: the anchor itself when it is
 *  the button, else the button inside it. A picker's anchor is often a
 *  wrapper as wide as its row, and a menu placed by the wrapper opened at
 *  the row's far end, away from the button that was pressed. */
function triggerOf(anchor: HTMLElement): HTMLElement {
  if (anchor.matches("button, [aria-haspopup]")) {
    return anchor;
  }
  return anchor.querySelector<HTMLElement>("[aria-haspopup], button") ?? anchor;
}

/** closesOn reports whether a key pressed while a menu is open dismisses
 *  it: Escape, which also gives the focus back to the trigger. */
export function closesOn(key: string): boolean {
  return key === "Escape";
}

/**
 * Dropdown renders a menu in a body portal so it is never clipped by a
 * scrolling/overflow ancestor. It opens at the pressed control (placePopup):
 * below it, or above when the room below is short; clamped inside the
 * viewport and inside the box marked data-popup-bounds it was opened from
 * (the card drawer); and no taller than the room it opened into, scrolling
 * inside. It is placed again whenever its content or the page moves.
 */
export function Dropdown({ open, anchorRef, onClose, className, role, align, children }: DropdownProps) {
  const menuRef = useRef<HTMLDivElement | null>(null);
  // The menu's own height cap from its stylesheet, read before any inline
  // max-height of ours hides it.
  const capRef = useRef<number | null>(null);
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
    if (capRef.current === null) {
      const css = parseFloat(getComputedStyle(menu).maxHeight);
      capRef.current = Number.isFinite(css) ? css : Infinity;
    }
    const a = triggerOf(anchor).getBoundingClientRect();
    // A control out of the layout measures as an empty box at 0,0: the menu
    // keeps the place it has (or stays hidden until it gets one) rather than
    // being thrown into the screen's top-left corner.
    if (!anchorShown(a)) {
      return;
    }
    const m = menu.getBoundingClientRect();
    const boundsEl = anchor.closest<HTMLElement>("[data-popup-bounds]");
    const p = placePopup(
      a,
      { width: m.width, height: Math.min(menu.scrollHeight, capRef.current) },
      { width: window.innerWidth, height: window.innerHeight },
      { align, bounds: boundsEl?.getBoundingClientRect() },
    );
    setStyle({
      position: "fixed",
      top: p.top,
      left: p.left,
      right: "auto",
      maxHeight: Math.min(p.maxHeight, capRef.current),
      visibility: "visible",
      // The same layer the hidden state starts on: this call writes the
      // style afresh, so a lower one here puts the menu back under the card
      // modal it was opened over.
      zIndex: 200,
    });
  }, [anchorRef, align]);

  useLayoutEffect(() => {
    if (open) {
      place();
    } else {
      // A closed menu forgets where it stood, so the next opening is never
      // shown at the last one's place before it is given its own.
      setStyle((s) => (s.visibility === "hidden" ? s : { ...s, visibility: "hidden" }));
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
    // Escape dismisses, and the focus goes back to the trigger — the menu
    // lives in a portal, so leaving it there would drop the focus on <body>.
    const onKey = (e: KeyboardEvent) => {
      if (!closesOn(e.key)) {
        return;
      }
      e.stopPropagation();
      onClose();
      const anchor = anchorRef.current;
      const trigger = anchor?.matches("button, [tabindex]") ? anchor : anchor?.querySelector<HTMLElement>("button, [tabindex]");
      trigger?.focus();
    };
    window.addEventListener("scroll", reposition, true);
    window.addEventListener("resize", reposition);
    // A picker's list fills in after it opens (the options load, a search
    // narrows them): the menu is placed again as its size changes, or it
    // keeps the place it was given while empty and runs off the screen.
    const grow = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(reposition);
    if (menuRef.current) {
      grow?.observe(menuRef.current);
      const list = menuRef.current.firstElementChild;
      if (list) {
        grow?.observe(list);
      }
    }
    document.addEventListener("pointerdown", onDown, true);
    document.addEventListener("keydown", onKey, true);
    return () => {
      window.removeEventListener("scroll", reposition, true);
      window.removeEventListener("resize", reposition);
      grow?.disconnect();
      document.removeEventListener("pointerdown", onDown, true);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [open, place, onClose, anchorRef]);

  if (!open) {
    return null;
  }
  return createPortal(
    <div ref={menuRef} className={className} style={style} role={role}>
      {children}
    </div>,
    document.body,
  );
}
