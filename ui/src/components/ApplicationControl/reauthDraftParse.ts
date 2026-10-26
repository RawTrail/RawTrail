// Parsers for the application control dialogs' saved work (issue #1113). Storage can hold whatever was written under a key, so each
// dialog accepts a payload only when it is of its own shape.

import type { Enforcement } from "../../api";

// parseEnforcement reads a dialog's enforcement choice, which is null until the operator picks one. It returns undefined for anything
// else, which is a payload that is not the dialog's own, so null is a valid result and undefined is the refusal.
export function parseEnforcement(raw: unknown): Enforcement | null | undefined {
  if (raw === null) return null;
  return raw === "PROTECT" || raw === "DETECT" ? raw : undefined;
}
