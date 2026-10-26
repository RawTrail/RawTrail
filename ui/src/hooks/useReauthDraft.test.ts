import { describe, it, expect, afterEach, beforeEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { useReauthDraft } from "./useReauthDraft";
import { hasUnsavedWork, persistReauthDrafts, readReauthDraft, setReauthDraftUser } from "../reauthDraft";

// Accepts only this test surface's own shape.
function parseText(raw: unknown): { text: string } | undefined {
  if (typeof raw !== "object" || raw === null) return undefined;
  const { text } = raw as Record<string, unknown>;
  return typeof text === "string" ? { text } : undefined;
}

// useReauthDraft is how a surface keeps unsaved work across an OIDC reauthentication (issue #1113). The surface says what its work
// is; the reauthentication dialog saves it; on return the surface gets it back once.

beforeEach(() => {
  sessionStorage.clear();
  setReauthDraftUser(1);
});

afterEach(() => {
  sessionStorage.clear();
  setReauthDraftUser(null);
});

describe("useReauthDraft", () => {
  it("offers the surface's work to the dialog as of when the dialog asks", () => {
    const { rerender } = renderHook(({ work }) => useReauthDraft<{ text: string }>("surface", work, parseText), {
      initialProps: { work: null as { text: string } | null },
    });
    expect(hasUnsavedWork()).toBe(false);

    rerender({ work: { text: "typed" } });
    expect(hasUnsavedWork()).toBe(true);
    persistReauthDrafts();
    expect(readReauthDraft("surface")).toEqual({ text: "typed" });
  });

  it("offers nothing once the surface has no work again", () => {
    const workOf = (text: string | null): { text: string } | null => (text === null ? null : { text });
    const { rerender } = renderHook(({ work }: { work: { text: string } | null }) => useReauthDraft("surface", work, parseText), {
      initialProps: { work: workOf("typed") },
    });
    rerender({ work: null });
    persistReauthDrafts();
    expect(readReauthDraft("surface")).toBeUndefined();
  });

  it("stops offering work when the surface unmounts", () => {
    const { unmount } = renderHook(() => useReauthDraft("surface", { text: "typed" }, parseText));
    expect(hasUnsavedWork()).toBe(true);
    unmount();
    expect(hasUnsavedWork()).toBe(false);
  });

  // spec:web-ui/unsaved-work-survives-a-reauthentication/unsaved-work-is-restored-and-not-saved-for-the-operator
  it("hands back the work saved before the last reauthentication, and removes it from storage when consumed", () => {
    const first = renderHook(() => useReauthDraft("surface", { text: "typed" }, parseText));
    persistReauthDrafts();
    first.unmount();

    const second = renderHook(() => useReauthDraft<{ text: string }>("surface", null, parseText));
    expect(second.result.current.restored).toEqual({ text: "typed" });
    expect(readReauthDraft("surface")).toEqual({ text: "typed" });

    second.result.current.consume();
    expect(readReauthDraft("surface")).toBeUndefined();
    // Held for the life of the component, so the surface applies it once on its own record of having done so.
    expect(second.result.current.restored).toEqual({ text: "typed" });
  });

  it("restores nothing when nothing was saved", () => {
    const { result } = renderHook(() => useReauthDraft("surface", null, parseText));
    expect(result.current.restored).toBeUndefined();
  });

  it("does not hand one surface another's work", () => {
    const first = renderHook(() => useReauthDraft("rule-editor:a", { text: "a's work" }, parseText));
    persistReauthDrafts();
    first.unmount();

    const other = renderHook(() => useReauthDraft("rule-editor:b", null, parseText));
    expect(other.result.current.restored).toBeUndefined();
  });

  // spec:web-ui/unsaved-work-survives-a-reauthentication/saved-work-is-not-restored-when-it-is-stale-or-from-another-page
  it("refuses a payload of another shape under a valid envelope, and removes it", () => {
    for (const payload of [null, "text", 42, { text: 7 }, {}]) {
      sessionStorage.setItem(
        "rawtrail:reauth-draft:surface",
        JSON.stringify({ savedAt: Date.now(), path: "/", userId: 1, value: payload }),
      );

      const { result, unmount } = renderHook(() => useReauthDraft("surface", null, parseText));

      expect(result.current.restored).toBeUndefined();
      expect(sessionStorage.getItem("rawtrail:reauth-draft:surface")).toBeNull();
      unmount();
    }
  });
});
