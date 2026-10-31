import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { ProfileProvider } from "./profile";
import { ADVANCED_KEY, toProfile, useProfile, type Profile } from "./profile-core";

function wrapper(profile: Profile) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <ProfileProvider profile={profile}>{children}</ProfileProvider>;
  };
}

beforeEach(() => { localStorage.removeItem(ADVANCED_KEY); });
afterEach(() => { vi.restoreAllMocks(); });

describe("toProfile", () => {
  it("reads laptop as laptop and anything else, or nothing, as a server", () => {
    expect(toProfile("laptop")).toBe("laptop");
    expect(toProfile("server")).toBe("server");
    expect(toProfile(undefined)).toBe("server");
  });
});

describe("ProfileProvider", () => {
  it("always shows advanced settings on a server", () => {
    localStorage.setItem(ADVANCED_KEY, "0");
    const { result } = renderHook(() => useProfile(), { wrapper: wrapper("server") });
    expect(result.current).toMatchObject({ profile: "server", showAdvanced: true });
  });

  it("starts a laptop with them hidden, and keeps the person's choice", () => {
    const { result } = renderHook(() => useProfile(), { wrapper: wrapper("laptop") });
    expect(result.current.showAdvanced).toBe(false);
    act(() => { result.current.setShowAdvanced(true); });
    expect(result.current.showAdvanced).toBe(true);
    expect(localStorage.getItem(ADVANCED_KEY)).toBe("1");
    act(() => { result.current.setShowAdvanced(false); });
    expect(localStorage.getItem(ADVANCED_KEY)).toBe("0");
  });
});
