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
    const right = Math.max(8, window.innerWidth - a.right);
    let top = a.bottom + gap;
    if (top + m.height > window.innerHeight - 8) {
      top = Math.max(8, a.top - gap - m.height);
    }
    setStyle({ position: "fixed", top, right, visibility: "visible", zIndex: 60 });
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
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node;
      if (menuRef.current?.contains(t) || anchorRef.current?.contains(t)) {
        return;
      }
      onClose();
    };
    window.addEventListener("scroll", reposition, true);
    window.addEventListener("resize", reposition);
    document.addEventListener("mousedown", onDown);
    return () => {
      window.removeEventListener("scroll", reposition, true);
      window.removeEventListener("resize", reposition);
      document.removeEventListener("mousedown", onDown);
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
