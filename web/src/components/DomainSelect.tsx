import type { FocusEvent } from "react";
import { writableDomains, type DomainInfo } from "../domains";

interface DomainSelectProps {
  domains: readonly DomainInfo[];
  /** The pick so far ("" = none yet: the first writable domain shows). */
  value: string;
  onChange: (domain: string) => void;
  className?: string;
}

const CLASS = "domain-select";

/** DomainSelect picks the repository a new team / project / process is
 *  declared in. It renders nothing unless the visitor can write to more than
 *  one — a one-repository board never sees it. */
export function DomainSelect({ domains, value, onChange, className }: DomainSelectProps) {
  const options = writableDomains(domains);
  if (options.length < 2) {
    return null;
  }
  return (
    <select
      className={className ? `${CLASS} ${className}` : CLASS}
      value={value || options[0].name}
      title="Repository the new entry is declared in"
      aria-label="Repository"
      onChange={(e) => onChange(e.target.value)}
      onClick={(e) => e.stopPropagation()}
      onPointerDown={(e) => e.stopPropagation()}
    >
      {options.map((d) => (
        <option key={d.name} value={d.name}>
          {d.name}
        </option>
      ))}
    </select>
  );
}

/** blurredIntoDomainSelect tells an input that commits on blur that the focus
 *  only moved to the domain selector beside it — the entry is still being
 *  made, so nothing should be committed or closed yet. */
export function blurredIntoDomainSelect(e: FocusEvent<HTMLElement>): boolean {
  const to = e.relatedTarget as HTMLElement | null;
  return !!to?.classList?.contains(CLASS);
}
