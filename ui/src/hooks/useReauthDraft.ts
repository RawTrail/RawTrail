// useReauthDraft is how a surface keeps its unsaved work across an OIDC reauthentication (issue #1113; see reauthDraft.ts for the
// pattern). The surface says what its unsaved work is, as a JSON-serializable value, or null when it has none; the reauthentication
// dialog saves it just before it leaves the page. On return the surface gets the saved value back as `restored`, applies it, and calls
// `consume`. It never gets the mutation back: the operator reviews what was restored and saves again.

import { useCallback, useEffect, useRef, useState } from "react";
import { discardReauthDraft, readReauthDraft, registerReauthDraft } from "../reauthDraft";

export interface UseReauthDraft<T> {
  // restored is the work saved before the last reauthentication, or undefined when there is none to restore. It holds for the life of
  // the component, so the surface applies it once and keeps its own record of having done so.
  restored: T | undefined;
  // consume removes the saved work from storage. Call it once the work has been applied, so a reload does not apply it again.
  consume: () => void;
}

// key names the surface, and includes whatever distinguishes one instance of it from another (the rule being edited, say), so work
// saved from one is not restored into the other.
//
// parse decides whether what came out of storage is this surface's work, and returns it typed, or undefined when it is not. Storage
// holds whatever was written under the key, and an entry that passes the envelope checks can still carry a payload of another shape
// (an older build's, or a hand-edited one), which a surface must not be handed as if it were its own. A payload parse refuses is
// removed, not left to be refused again on every load.
export function useReauthDraft<T>(key: string, unsaved: T | null, parse: (raw: unknown) => T | undefined): UseReauthDraft<T> {
  // The latest unsaved work, read when the dialog asks rather than when it was last rendered, which is why this is a ref.
  const latest = useRef<T | null>(unsaved);
  useEffect(() => {
    latest.current = unsaved;
  });
  useEffect(() => registerReauthDraft(key, () => latest.current), [key]);

  // Read once, on mount: reading does not remove it, so a render React runs twice sees the same draft.
  const [restored] = useState<T | undefined>(() => {
    const raw = readReauthDraft(key);
    if (raw === undefined) return undefined;
    const parsed = parse(raw);
    if (parsed === undefined) discardReauthDraft(key);
    return parsed;
  });
  const consume = useCallback(() => {
    discardReauthDraft(key);
  }, [key]);
  return { restored, consume };
}
