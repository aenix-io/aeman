import { describe, expect, it } from "vitest";

import { eventLabel } from "./eventlog";
import type { CardEvent } from "./providers/types";

const ev = (kind: string, from: string, to: string) => ({ kind, from, to }) as CardEvent;

// The activity log is the second documentation; the labels of the
// placement events read as the actions they record.
describe("eventLabel", () => {
  it("words a column move by its columns", () => {
    expect(eventLabel(ev("epic", "engineering / Cozystack", "freedom / Launch"))).toBe(
      "column engineering / Cozystack → freedom / Launch",
    );
  });

  it("words a mirror by its direction", () => {
    expect(eventLabel(ev("mirror", "", "freedom / Launch"))).toBe("mirrored to freedom / Launch");
    expect(eventLabel(ev("mirror", "freedom / Launch", ""))).toBe(
      "no longer mirrored to freedom / Launch",
    );
    // A rename's rewrite carries both sides and reads as the new address.
    expect(eventLabel(ev("mirror", "freedom / Launch", "freedom / Liftoff"))).toBe(
      "mirrored to freedom / Liftoff",
    );
  });

  it("words a process tie by its direction", () => {
    expect(eventLabel(ev("process", "", "Invoicing"))).toBe("tied to the process Invoicing");
    expect(eventLabel(ev("process", "Invoicing", ""))).toBe("untied from the process Invoicing");
  });
});
