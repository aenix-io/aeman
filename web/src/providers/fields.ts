import type { Board, FieldRoles } from "./types";

// Aliases map a field role to the field names (case-insensitive) that fill it.
const ALIASES: Record<keyof FieldRoles, string[]> = {
  zone: ["zone", "priority zone", "зона"],
  progress: ["progress", "readiness", "% done", "percent", "готовность"],
  day: ["day", "date", "due date", "due", "день", "дата"],
  sprint: ["sprint", "iteration", "спринт", "итерация"],
  status: ["status", "статус"],
  stage: ["stage", "состояние"],
};

/** fieldRoles maps a board's fields onto well-known roles by name. */
export function fieldRoles(board: Board): FieldRoles {
  const roles: FieldRoles = {};
  for (const field of board.fields) {
    const name = field.name.trim().toLowerCase();
    (Object.keys(ALIASES) as (keyof FieldRoles)[]).forEach((role) => {
      if (!roles[role] && ALIASES[role].includes(name)) {
        roles[role] = field;
      }
    });
  }
  return roles;
}
