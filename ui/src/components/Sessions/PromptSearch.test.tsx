import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { PromptSearch } from "./PromptSearch";
import * as api from "../../api";
import type { FoundAgentPrompt } from "../../types";

const DAY_NS = 24 * 3_600_000 * 1_000_000;

function found(over: Partial<FoundAgentPrompt> = {}): FoundAgentPrompt {
  return {
    host_id: "H1", agent_session_id: "169b5bb1-29b5-41da-9a54-92cd72599ad9", prompt_id: "turn-1",
    text: "please deploy to prod tonight", length: 29, at_ns: 1_000_000_000, uid: 501, ...over,
  };
}

function renderSearch() {
  return render(<MemoryRouter><PromptSearch /></MemoryRouter>);
}

function search(text: string) {
  fireEvent.change(screen.getByRole("searchbox", { name: "Prompt text" }), { target: { value: text } });
  fireEvent.click(screen.getByRole("button", { name: "Search" }));
}

afterEach(() => { vi.restoreAllMocks(); });

describe("PromptSearch", () => {
  // spec:web-ui/agent-prompt-search/an-operator-finds-a-prompt-across-sessions
  it("searches the window chosen and lists each prompt with its host, the match marked, linking to its conversation", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([{ host_id: "H1", hostname: "mac-a.local" } as never]);
    vi.spyOn(Date, "now").mockReturnValue(40 * 24 * 3_600_000);
    const spy = vi.spyOn(api, "searchAgentPrompts").mockResolvedValue({ prompts: [found()] });
    renderSearch();
    fireEvent.change(screen.getByRole("combobox", { name: "Prompts typed in" }), { target: { value: "30" } });
    search("  Deploy ");
    await waitFor(() => { expect(spy).toHaveBeenCalled(); });
    expect(spy.mock.calls[0][0]).toEqual({ q: "Deploy", from: String(10 * DAY_NS), to: String(40 * DAY_NS), cursor: undefined });
    expect(await screen.findByText("mac-a.local")).toBeVisible();
    expect(screen.getByText("deploy", { selector: "mark" })).toBeVisible();
    expect(screen.getByRole("link")).toHaveAttribute(
      "href", "/agent-sessions/H1/169b5bb1-29b5-41da-9a54-92cd72599ad9?view=conversation");
    expect(screen.getByText("Every search is recorded in the audit log with its text.")).toBeVisible();
  });

  it("pages the same window, and says when nothing matches", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(Date, "now").mockReturnValue(40 * 24 * 3_600_000);
    const spy = vi.spyOn(api, "searchAgentPrompts")
      .mockResolvedValueOnce({ prompts: [found()], next_cursor: "c2" })
      .mockResolvedValueOnce({ prompts: [found({ prompt_id: "turn-2", text: "deploy again" })] });
    renderSearch();
    search("deploy");
    const more = await screen.findByRole("button", { name: "Load more" });
    vi.spyOn(Date, "now").mockReturnValue(41 * 24 * 3_600_000);
    fireEvent.click(more);
    expect(await screen.findByText("again", { exact: false })).toBeVisible();
    expect(spy.mock.calls[1][0]).toEqual({ ...spy.mock.calls[0][0], cursor: "c2" });

    spy.mockResolvedValueOnce({ prompts: [] });
    search("nothing like it");
    expect(await screen.findByText("No prompts match.")).toBeVisible();
  });

  it("does not search for fewer than three characters", () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    const spy = vi.spyOn(api, "searchAgentPrompts");
    renderSearch();
    fireEvent.change(screen.getByRole("searchbox", { name: "Prompt text" }), { target: { value: " ab " } });
    expect(screen.getByRole("button", { name: "Search" })).toBeDisabled();
    fireEvent.submit(screen.getByRole("searchbox", { name: "Prompt text" }));
    expect(spy).not.toHaveBeenCalled();
  });

  it("says so when the search fails", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "searchAgentPrompts").mockRejectedValue(new Error("bad_window"));
    renderSearch();
    search("deploy");
    expect(await screen.findByText("Error: bad_window")).toBeVisible();
  });
});
