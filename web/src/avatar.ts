/** initials reduces a login to one or two uppercase characters for an avatar. */
export function initials(login: string): string {
  const parts = login.split(/[-_.\s]+/).filter(Boolean);
  if (parts.length >= 2) {
    return (parts[0][0] + parts[1][0]).toUpperCase();
  }
  const clean = login.replace(/[^A-Za-z0-9]/g, "");
  return (clean.slice(0, 2) || login.slice(0, 2)).toUpperCase();
}

/** teamInitial returns a single uppercase letter for a compact team badge. */
export function teamInitial(name: string): string {
  const clean = name.replace(/[^A-Za-z0-9]/g, "");
  return (clean[0] ?? name[0] ?? "?").toUpperCase();
}

/** teamColor returns a deterministic colour derived from a team name. */
export function teamColor(name: string): string {
  let hash = 0;
  for (let i = 0; i < name.length; i += 1) {
    hash = (hash * 31 + name.charCodeAt(i)) | 0;
  }
  return `hsl(${Math.abs(hash) % 360}, 55%, 45%)`;
}
