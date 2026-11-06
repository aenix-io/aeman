// What each board's filter dropdown (components/FilterSelect) offers. The
// no-team group is a team like any other in every team filter (ownership.md,
// O11): "No team" is an option, the empty value, as it is the empty member
// of the selector's team set.
import type { FilterOption } from "./components/FilterSelect";
import type { CustomerRef } from "./providers/types";

/** teamOptions is the roster and the no-team group, last. */
export function teamOptions(roster: readonly string[]): FilterOption[] {
  return [...roster.filter((t) => t !== "").map((t) => ({ value: t, label: t })), { value: "", label: "No team" }];
}

/** customerOptions is the customer roster, by name, keyed by id. */
export function customerOptions(customers: readonly CustomerRef[] | undefined): FilterOption[] {
  return (customers ?? []).map((c) => ({ value: c.id, label: c.name }));
}

/** projectOptions is the projects, and "No project" when columns with no
 *  project exist to be shown by it. */
export function projectOptions(projects: readonly string[], loose: boolean): FilterOption[] {
  const out = projects.map((p) => ({ value: p, label: p }));
  return loose ? [...out, { value: "", label: "No project" }] : out;
}
