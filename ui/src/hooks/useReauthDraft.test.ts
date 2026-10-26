import { describe, it, expect, afterEach, beforeEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { useReauthDraft } from "./useReauthDraft";
import { hasUnsavedWork, persistReauthDrafts, readReauthDraft } from "../reauthDraft";

// useReauthDraft is how a surface keeps unsaved work across an OIDC reauthentication (issue #1113). The surface says what its work
// is; the reauthentication dialog saves it; on return the surface gets it back once.

beforeEach(() => {
  sessionStorage.clear();
});

afterEach(() => {
  sessionStorage.clear();
});

describe("useReauthDraft", () => {
  it("offers the surface's work to the dialog as of when the dialog asks", () => {
    const { rerender } = renderHook(({ work }) => useReauthDraft<{ text: string }>("surface", work), {
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
    const { rerender } = renderHook(({ work }: { work: { text: string } | null }) => useReauthDraft("surface", work), {
      initialProps: { work: workOf("typed") },
    });
    rerender({ work: null });
    persistReauthDrafts();
    expect(readReauthDraft("surface")).toBeUndefined();
  });

  it("stops offering work when the surface unmounts", () => {
    const { unmount } = renderHook(() => useReauthDraft("surface", { text: "typed" }));
    expect(hasUnsavedWork()).toBe(true);
    unmount();
    expect(hasUnsavedWork()).toBe(false);
  });

  // spec:web-ui/unsaved-work-survives-a-reauthentication/unsaved-work-is-restored-and-not-saved-for-the-operator
  it("hands back the work saved before the last reauthentication, and removes it from storage when consumed", () => {
    const first = renderHook(() => useReauthDraft("surface", { text: "typed" }));
    persistReauthDrafts();
    first.unmount();

    const second = renderHook(() => useReauthDraft<{ text: string }>("surface", null));
    expect(second.result.current.restored).toEqual({ text: "typed" });
    expect(readReauthDraft("surface")).toEqual({ text: "typed" });

    second.result.current.consume();
    expect(readReauthDraft("surface")).toBeUndefined();
    // Held for the life of the component, so the surface applies it once on its own record of having done so.
    expect(second.result.current.restored).toEqual({ text: "typed" });
  });

  it("restores nothing when nothing was saved", () => {
    const { result } = renderHook(() => useReauthDraft("surface", null));
    expect(result.current.restored).toBeUndefined();
  });

  it("does not hand one surface another's work", () => {
    const first = renderHook(() => useReauthDraft("rule-editor:a", { text: "a's work" }));
    persistReauthDrafts();
    first.unmount();

    const other = renderHook(() => useReauthDraft("rule-editor:b", null));
    expect(other.result.current.restored).toBeUndefined();
  });
});
