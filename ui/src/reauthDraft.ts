// reauthDraft keeps an operator's unsaved work across an OIDC reauthentication, which leaves the page for the identity provider and
// so destroys everything the page held in memory (issue #1113).
//
// The pattern is the usual one for step-up authentication. The input is held in the browser, in sessionStorage, which survives the
// redirect round trip and goes when the tab does. It is restored on the page the operator returns to, with a notice that says so. And
// it is never replayed: the mutation that triggered the reauthentication is not run again on return, because an action gated on fresh
// authentication is one the operator should look at again, after a round trip through the identity provider, before it lands. The
// operator reviews the restored input and saves it again.
//
// A surface opts in with useReauthDraft, saying what its unsaved work is. Only what an operator typed belongs here. Nothing secret
// should be registered: a draft sits in storage in the clear until it is restored, discarded, or older than DRAFT_TTL_MS.

// DRAFT_TTL_MS is how long a saved draft stays restorable. A reauthentication takes a minute or two, so anything much older is not
// work interrupted by one but a draft left behind, and restoring it into a page long since reloaded would surprise rather than help.
const DRAFT_TTL_MINUTES = 15;
const MS_PER_MINUTE = 60_000;
export const DRAFT_TTL_MS = DRAFT_TTL_MINUTES * MS_PER_MINUTE;

const KEY_PREFIX = "rawtrail:reauth-draft:";

// A surface's current unsaved work, or null when it has none. Null is what keeps a clean page out of storage.
type DraftSource = () => unknown;

// The page's registered surfaces. Module state rather than context because registration is a side channel from a surface to the
// reauthentication dialog, which render in different parts of the tree with no common ancestor that wants to know.
const sources = new Map<string, DraftSource>();

interface StoredDraft {
  readonly savedAt: number;
  readonly path: string;
  // The operator the work belongs to. A reauthentication through the identity provider can complete as a different account, and work
  // typed by one operator must not be put into another's editor.
  readonly userId: number;
  readonly value: unknown;
}

// The signed-in operator's id, set by the app once the session is known and cleared when it ends. Draft storage is keyed to it: nothing
// is saved without one, and a draft is restored only to the operator who wrote it. The session id would not do, since it changes
// across a reauthentication, while the user id is what stays the same when the same person signs in again.
let currentUserId: number | null = null;

// setReauthDraftUser records who is signed in, or null when nobody is.
export function setReauthDraftUser(userId: number | null): void {
  currentUserId = userId;
}

// isWork is whether a surface's value is unsaved work: null, or undefined from a surface that has none, is not.
function isWork(value: unknown): boolean {
  return value !== null && value !== undefined;
}

function storage(): Storage | null {
  try {
    return globalThis.sessionStorage;
  } catch {
    // Blocked storage, as in some private windows. Work is then not kept, and the dialog says so.
    return null;
  }
}

// registerReauthDraft makes a surface's unsaved work available to persistReauthDrafts, and returns what unregisters it.
export function registerReauthDraft(key: string, source: DraftSource): () => void {
  sources.set(key, source);
  return () => {
    // Only this registration: a surface remounted under the same key must not lose the newer one to the older's cleanup.
    if (sources.get(key) === source) sources.delete(key);
  };
}

// hasUnsavedWork reports whether any registered surface holds unsaved work, which is what the reauthentication dialog warns about.
export function hasUnsavedWork(): boolean {
  for (const source of sources.values()) {
    if (isWork(source())) return true;
  }
  return false;
}

// persistReauthDrafts saves the unsaved work of every registered surface, ahead of the redirect. It reports whether it could, so the
// dialog is not left claiming work was kept when storage refused it.
export function persistReauthDrafts(now: number = Date.now()): boolean {
  const store = storage();
  if (store === null) return !hasUnsavedWork();
  const path = globalThis.location.pathname;
  const userId = currentUserId;
  // Nobody signed in, so nobody the work could be given back to.
  if (userId === null) return !hasUnsavedWork();
  try {
    for (const [key, source] of sources) {
      const value = source();
      if (!isWork(value)) continue;
      const entry: StoredDraft = { savedAt: now, path, userId, value };
      store.setItem(KEY_PREFIX + key, JSON.stringify(entry));
    }
    return true;
  } catch {
    // Quota, or storage that throws on write.
    return false;
  }
}

function isStoredDraft(raw: unknown): raw is StoredDraft {
  if (typeof raw !== "object" || raw === null) return false;
  const e = raw as Record<string, unknown>;
  return typeof e.savedAt === "number" && typeof e.path === "string" && typeof e.userId === "number" && "value" in e;
}

// readReauthDraft returns the draft saved under key, or undefined when there is none, it is older than DRAFT_TTL_MS, it was saved on
// another page, or it cannot be read. A draft that is too old or unreadable is removed. Reading does not remove a good one: a
// surface removes it with discardReauthDraft once it has applied it, so a render that runs twice does not lose it.
export function readReauthDraft(key: string, now: number = Date.now()): unknown {
  const store = storage();
  if (store === null) return undefined;
  let text: string | null;
  try {
    text = store.getItem(KEY_PREFIX + key);
  } catch {
    return undefined;
  }
  if (text === null) return undefined;
  let entry: unknown;
  try {
    entry = JSON.parse(text);
  } catch {
    discardReauthDraft(key);
    return undefined;
  }
  if (!isStoredDraft(entry) || now - entry.savedAt > DRAFT_TTL_MS || entry.savedAt > now) {
    discardReauthDraft(key);
    return undefined;
  }
  // Another operator's work is dropped rather than kept: it was not written by the person now at the keyboard, and it is not theirs to
  // see. Nor is it restored when nobody is known to be signed in.
  if (currentUserId === null || entry.userId !== currentUserId) {
    discardReauthDraft(key);
    return undefined;
  }
  // Left in place for the page it belongs to: a draft is restored where it was made, not on whichever page registers the key next.
  if (entry.path !== globalThis.location.pathname) return undefined;
  return entry.value;
}

// discardReauthDraft removes the draft saved under key.
export function discardReauthDraft(key: string): void {
  try {
    storage()?.removeItem(KEY_PREFIX + key);
  } catch {
    // Nothing to remove from storage that will not let go of it.
  }
}

// hasReauthDraft reports whether work saved under key could be restored now: a page uses it to open the dialog the work belongs to
// before the dialog reads it. It applies the same rules as readReauthDraft, including dropping work that is stale or another operator's.
export function hasReauthDraft(key: string, now: number = Date.now()): boolean {
  return readReauthDraft(key, now) !== undefined;
}
