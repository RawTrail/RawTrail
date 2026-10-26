import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import {
  DRAFT_TTL_MS,
  discardReauthDraft,
  hasUnsavedWork,
  persistReauthDrafts,
  readReauthDraft,
  registerReauthDraft,
  setReauthDraftUser,
} from "./reauthDraft";

// reauthDraft keeps unsaved work across an OIDC reauthentication, which leaves the page (issue #1113). These tests pin the storage
// contract: what is written, what is read back, and what is deliberately refused.

const KEY = "rawtrail:reauth-draft:";
const unregister: (() => void)[] = [];

function register(key: string, value: unknown): () => void {
  const off = registerReauthDraft(key, () => value);
  unregister.push(off);
  return off;
}

beforeEach(() => {
  sessionStorage.clear();
  setReauthDraftUser(1);
});

afterEach(() => {
  // Stubs first: clearing storage through a stub that has no clear() would throw and leave every later test on the stub.
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  while (unregister.length > 0) unregister.pop()?.();
  sessionStorage.clear();
  setReauthDraftUser(null);
});

describe("persistReauthDrafts", () => {
  // spec:web-ui/unsaved-work-survives-a-reauthentication/unsaved-work-is-kept-across-the-sign-in
  it("writes the work of every registered surface that has some, and nothing for one that has none", () => {
    register("a", { text: "typed" });
    register("b", null);
    register("c", undefined);

    expect(persistReauthDrafts(1000)).toBe(true);

    expect(JSON.parse(sessionStorage.getItem(`${KEY}a`) ?? "null")).toEqual({ savedAt: 1000, path: "/", userId: 1, value: { text: "typed" } });
    expect(sessionStorage.getItem(`${KEY}b`)).toBeNull();
    expect(sessionStorage.getItem(`${KEY}c`)).toBeNull();
  });

  it("reads the work when asked, not when it registered", () => {
    let value: unknown = null;
    unregister.push(registerReauthDraft("late", () => value));
    value = { later: true };

    persistReauthDrafts();

    expect(readReauthDraft("late")).toEqual({ later: true });
  });

  it("keeps nothing, and says so, when work exists but nobody is known to be signed in", () => {
    register("a", { text: "typed" });
    setReauthDraftUser(null);

    expect(persistReauthDrafts()).toBe(false);
    expect(sessionStorage.getItem(`${KEY}a`)).toBeNull();
  });

  it("says it could not keep the work when storage refuses it", () => {
    register("a", { text: "typed" });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("full", "QuotaExceededError");
    });

    expect(persistReauthDrafts()).toBe(false);
  });

  it("does not report a loss when there was nothing to keep", () => {
    register("a", null);
    vi.stubGlobal("sessionStorage", {
      getItem: () => { throw new Error("blocked"); },
      setItem: () => { throw new Error("blocked"); },
      removeItem: () => { throw new Error("blocked"); },
    });

    expect(persistReauthDrafts()).toBe(true);
  });

  it("reports a loss when storage is blocked and there was work", () => {
    register("a", { text: "typed" });
    vi.stubGlobal("sessionStorage", {
      getItem: () => { throw new Error("blocked"); },
      setItem: () => { throw new Error("blocked"); },
      removeItem: () => { throw new Error("blocked"); },
    });

    expect(persistReauthDrafts()).toBe(false);
    expect(readReauthDraft("a")).toBeUndefined();
  });
});

describe("hasUnsavedWork", () => {
  it("is true only while some registered surface has work", () => {
    expect(hasUnsavedWork()).toBe(false);
    register("clean", null);
    expect(hasUnsavedWork()).toBe(false);
    register("dirty", { x: 1 });
    expect(hasUnsavedWork()).toBe(true);
  });

  it("stops counting a surface once it unregisters", () => {
    const off = register("dirty", { x: 1 });
    off();
    expect(hasUnsavedWork()).toBe(false);
  });

  it("keeps a newer registration under the same key when the older one unregisters", () => {
    const older = register("same", { v: "old" });
    register("same", { v: "new" });
    older();
    expect(hasUnsavedWork()).toBe(true);
  });
});

describe("readReauthDraft", () => {
  function store(key: string, entry: unknown) {
    sessionStorage.setItem(KEY + key, typeof entry === "string" ? entry : JSON.stringify(entry));
  }

  // spec:web-ui/unsaved-work-survives-a-reauthentication/unsaved-work-is-restored-and-not-saved-for-the-operator
  it("returns the work and leaves it in place, so a render that runs twice sees it twice", () => {
    store("a", { savedAt: 1000, path: "/", userId: 1, value: { text: "typed" } });

    expect(readReauthDraft("a", 2000)).toEqual({ text: "typed" });
    expect(readReauthDraft("a", 2000)).toEqual({ text: "typed" });
  });

  // spec:web-ui/unsaved-work-survives-a-reauthentication/saved-work-is-not-restored-when-it-is-stale-or-from-another-page
  it("drops work older than the time to live", () => {
    store("a", { savedAt: 1000, path: "/", userId: 1, value: { text: "typed" } });

    expect(readReauthDraft("a", 1000 + DRAFT_TTL_MS)).toEqual({ text: "typed" });
    expect(readReauthDraft("a", 1001 + DRAFT_TTL_MS)).toBeUndefined();
    expect(sessionStorage.getItem(`${KEY}a`)).toBeNull();
  });

  it("drops work stamped in the future, which no clock of ours wrote", () => {
    store("a", { savedAt: 9000, path: "/", userId: 1, value: {} });
    expect(readReauthDraft("a", 1000)).toBeUndefined();
    expect(sessionStorage.getItem(`${KEY}a`)).toBeNull();
  });

  // spec:web-ui/unsaved-work-survives-a-reauthentication/saved-work-is-not-restored-when-it-is-stale-or-from-another-page
  it("does not restore work saved on another page, and keeps it for that page", () => {
    store("a", { savedAt: 1000, path: "/somewhere-else", userId: 1, value: { text: "typed" } });

    expect(readReauthDraft("a", 2000)).toBeUndefined();
    expect(sessionStorage.getItem(`${KEY}a`)).not.toBeNull();
  });

  it.each([
    ["not JSON", "{nope"],
    ["not an object", JSON.stringify("text")],
    ["missing its timestamp", JSON.stringify({ path: "/", userId: 1, value: {} })],
    ["missing its value", JSON.stringify({ savedAt: 1000, path: "/", userId: 1 })],
    ["missing its user", JSON.stringify({ savedAt: 1000, path: "/", value: {} })],
  ])("drops an entry that is %s", (_name, raw) => {
    store("a", raw);
    expect(readReauthDraft("a", 2000)).toBeUndefined();
    expect(sessionStorage.getItem(`${KEY}a`)).toBeNull();
  });

  // spec:web-ui/unsaved-work-survives-a-reauthentication/saved-work-is-not-restored-when-it-is-stale-or-from-another-page
  it("drops work written by a different operator, which a reauthentication that completed as someone else must not hand over", () => {
    store("a", { savedAt: 1000, path: "/", userId: 1, value: { text: "typed" } });
    setReauthDraftUser(2);

    expect(readReauthDraft("a", 2000)).toBeUndefined();
    expect(sessionStorage.getItem(`${KEY}a`)).toBeNull();
  });

  it("restores nothing when nobody is known to be signed in", () => {
    store("a", { savedAt: 1000, path: "/", userId: 1, value: { text: "typed" } });
    setReauthDraftUser(null);

    expect(readReauthDraft("a", 2000)).toBeUndefined();
  });

  it("returns nothing for a key never saved", () => {
    expect(readReauthDraft("never")).toBeUndefined();
  });
});

describe("discardReauthDraft", () => {
  it("removes the saved work", () => {
    sessionStorage.setItem(`${KEY}a`, "x");
    discardReauthDraft("a");
    expect(sessionStorage.getItem(`${KEY}a`)).toBeNull();
  });
});
