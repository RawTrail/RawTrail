import { describe, it, expect } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router";

import { useUrlFilter } from "./useUrlFilter";

function renderFilter(initial: string, keys: readonly string[]) {
  const wrapper = ({ children }: { children: ReactNode }) => createElement(MemoryRouter, { initialEntries: [initial] }, children);
  return renderHook(() => ({ filter: useUrlFilter([...keys]), location: useLocation() }), { wrapper });
}

describe("useUrlFilter", () => {
  it("reads only the named, non-empty params", () => {
    const { result } = renderFilter("/x?host_id=H1&agent=&other=1", ["host_id", "agent"]);
    expect(result.current.filter.activeFilter).toEqual({ host_id: "H1" });
    expect(result.current.filter.filterKey).toBe(JSON.stringify({ host_id: "H1" }));
  });

  it("sets and removes one param and keeps the others", () => {
    const { result } = renderFilter("/x?host_id=H1&view=table", ["host_id", "agent"]);
    act(() => { result.current.filter.setFilter("agent", "codex"); });
    expect(result.current.filter.activeFilter).toEqual({ host_id: "H1", agent: "codex" });
    act(() => { result.current.filter.setFilter("host_id", ""); });
    expect(result.current.filter.activeFilter).toEqual({ agent: "codex" });
    const params = new URLSearchParams(result.current.location.search);
    expect(params.has("host_id")).toBe(false);
    expect(params.get("view")).toBe("table");
  });

  it("keeps the same key across renders when the caller rebuilds its key list", () => {
    const { result, rerender } = renderFilter("/x?host_id=H1", ["host_id"]);
    const first = result.current.filter.activeFilter;
    rerender();
    expect(result.current.filter.activeFilter).toBe(first);
  });
});
