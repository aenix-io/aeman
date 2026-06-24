/** initials reduces a login to one or two uppercase characters for an avatar. */
export function initials(login: string): string {
  const parts = login.split(/[-_.\s]+/).filter(Boolean);
  if (parts.length >= 2) {
    return (parts[0][0] + parts[1][0]).toUpperCase();
  }
  const clean = login.replace(/[^A-Za-z0-9]/g, "");
  return (clean.slice(0, 2) || login.slice(0, 2)).toUpperCase();
}
