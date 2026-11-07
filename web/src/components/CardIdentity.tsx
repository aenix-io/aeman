import { useState } from "react";

type Clip = Pick<Clipboard, "writeText"> | undefined;

/** copyText puts text on the clipboard and says whether it got there: a
 *  board served over plain http has no clipboard, and a browser may refuse
 *  the write. */
export async function copyText(text: string, clipboard: Clip): Promise<boolean> {
  if (!clipboard) {
    return false;
  }
  try {
    await clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

type Copied = { what: "id" | "link"; ok: boolean } | null;

/** CardIdentity is the drawer's line naming its card: the id in mono —
 *  pressing it copies it — and "Copy link", the card's address on this board
 *  (deeplink.cardLink). The mark it leaves is timer-free, as ConnectDialog's:
 *  it stays on the one pressed until the other is, and the drawer keys this
 *  on the card, so another card starts afresh. It says "copied" only when
 *  the copy happened, and announces it. A card not made yet (a tmp- id) has
 *  no identity to give. */
export function CardIdentity({ uid, link }: { uid: string; link: string }) {
  const [copied, setCopied] = useState<Copied>(null);
  if (uid.startsWith("tmp-")) {
    return null;
  }
  const copy = (what: "id" | "link", text: string) => {
    void copyText(text, navigator.clipboard).then((ok) => setCopied({ what, ok }));
  };
  const mark = (what: "id" | "link") => (copied?.what === what ? (copied.ok ? "copied" : "copy failed") : null);
  return (
    <div className="drawer-identity">
      <button
        type="button"
        className="drawer-uid-btn"
        aria-label={`Copy card id ${uid}`}
        title={mark("id") ?? "Copy the card id"}
        onClick={() => copy("id", uid)}
      >
        <code className="drawer-uid">{uid}</code>
        <span className="drawer-copy-mark" aria-hidden="true">
          {mark("id") ?? "copy"}
        </span>
      </button>
      <button
        type="button"
        className="drawer-link-btn"
        aria-label="Copy the link to this card"
        title={link}
        onClick={() => copy("link", link)}
      >
        {mark("link") === "copied" ? "Link copied" : mark("link") === "copy failed" ? "Copy failed" : "Copy link"}
      </button>
      <span className="sr-only" role="status" aria-live="polite">
        {copied ? `${copied.what === "id" ? "Card id" : "Link"} ${copied.ok ? "copied" : "could not be copied"}` : ""}
      </span>
    </div>
  );
}
