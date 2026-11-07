import { useState } from "react";

/** CardIdentity is the drawer's line naming its card: the id in mono —
 *  pressing it copies it — and "Copy link", the card's address on this board
 *  (deeplink.cardLink). The "copied" mark is timer-free, as ConnectDialog's:
 *  it stays on the one pressed until the other is, and the drawer keys this
 *  on the card, so another card starts afresh. A card not made yet (a tmp-
 *  id) has no identity to give. */
export function CardIdentity({ uid, link }: { uid: string; link: string }) {
  const [copied, setCopied] = useState<"id" | "link" | null>(null);
  if (uid.startsWith("tmp-")) {
    return null;
  }
  const copy = (what: "id" | "link", text: string) => {
    void navigator.clipboard?.writeText(text);
    setCopied(what);
  };
  return (
    <div className="drawer-identity">
      <button
        type="button"
        className="drawer-uid-btn"
        aria-label="Copy the card id"
        title={copied === "id" ? "Copied" : "Copy the card id"}
        onClick={() => copy("id", uid)}
      >
        <code className="drawer-uid">{uid}</code>
        <span className="drawer-copy-mark" aria-hidden="true">
          {copied === "id" ? "copied" : "copy"}
        </span>
      </button>
      <button
        type="button"
        className="drawer-link-btn"
        aria-label="Copy the link to this card"
        title={link}
        onClick={() => copy("link", link)}
      >
        {copied === "link" ? "Link copied" : "Copy link"}
      </button>
    </div>
  );
}
