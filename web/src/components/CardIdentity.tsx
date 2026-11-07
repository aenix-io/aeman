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

/** CardIdentity is the drawer's pair of chips giving its card away: "Copy
 *  ID" and "Copy link", the card's address on this board (deeplink.cardLink).
 *  The id is not drawn — it is a thing to paste, not to read — but it names
 *  the first chip for a screen reader. The mark a press leaves is timer-free,
 *  as ConnectDialog's: it stays on the chip pressed until the other is, and
 *  the drawer keys this on the card, so another card starts afresh. It says
 *  "copied" only when the copy happened, and announces it. A card not made
 *  yet (a tmp- id) has nothing to give. */
export function CardIdentity({ uid, link }: { uid: string; link: string }) {
  const [copied, setCopied] = useState<Copied>(null);
  if (uid.startsWith("tmp-")) {
    return null;
  }
  const copy = (what: "id" | "link", text: string) => {
    void copyText(text, navigator.clipboard).then((ok) => setCopied({ what, ok }));
  };
  const label = (what: "id" | "link", idle: string) =>
    copied?.what === what ? (copied.ok ? "Copied" : "Copy failed") : idle;
  return (
    <div className="drawer-identity">
      <button
        type="button"
        className="drawer-copy-chip"
        aria-label={`Copy card id ${uid}`}
        title="Copy the card id"
        onClick={() => copy("id", uid)}
      >
        {label("id", "Copy ID")}
      </button>
      <button
        type="button"
        className="drawer-copy-chip"
        aria-label="Copy the link to this card"
        title={link}
        onClick={() => copy("link", link)}
      >
        {label("link", "Copy link")}
      </button>
      <span className="sr-only" role="status" aria-live="polite">
        {copied ? `${copied.what === "id" ? "Card id" : "Link"} ${copied.ok ? "copied" : "could not be copied"}` : ""}
      </span>
    </div>
  );
}
