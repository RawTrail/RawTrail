import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import type { AgentSessionDigest, AgentSessionTotals } from "../../types";
import { formatNs } from "../Search/format";
import { SessionSummary } from "./SessionSummary";

const MINUTE_NS = 60e9;

function digest(over: Partial<AgentSessionDigest> = {}): AgentSessionDigest {
  return {
    prompts: 0, programs: [], directories: [], directories_truncated: false, outside_project: 0, domains: [], domains_truncated: false,
    timeline: [0, 0], command_timeline: [0, 0], timeline_start_ns: 0, timeline_bucket_ns: 1, ...over,
  };
}

function totals(over: Partial<AgentSessionTotals> = {}): AgentSessionTotals {
  return {
    commands: 0, processes: 0, alerts: { total: 0, by_severity: {} }, tool_calls: { total: 0, by_tool: [] },
    activity: { files: { events: 0, distinct: 0, by_operation: {} }, network: { events: 0, distinct: 0 }, dns: { events: 0, distinct: 0 } },
    digest: digest(), ...over,
  };
}

function busy(): AgentSessionTotals {
  return totals({
    commands: 31,
    alerts: { total: 2, by_severity: { high: 2 } },
    tool_calls: { total: 12, by_tool: [{ tool: "Bash", count: 8 }, { tool: "Edit", count: 4 }] },
    activity: {
      files: { events: 9, distinct: 5, by_operation: { create: 2, modify: 6, delete: 1 } },
      network: { events: 3, distinct: 2 },
      dns: { events: 4, distinct: 3 },
    },
    usage: {
      models: [{ model: "claude-opus-5-5", messages: 4, input_tokens: 40, output_tokens: 2_000, cache_creation_input_tokens: 0,
        cache_read_input_tokens: 48_000, thinking_tokens: 0 }],
      modes: [{ mode: "bypassPermissions", messages: 4, first_ns: 0, last_ns: 10 * MINUTE_NS }],
    },
    digest: digest({
      prompts: 5,
      programs: [{ program: "git", count: 7 }, { program: "go", count: 3 }],
      directories: [{ directory: "/Users/dev/app/src", paths: 3, by_operation: { modify: 6 } },
        { directory: "/etc", paths: 1, by_operation: { create: 2 } }],
      directories_truncated: true,
      outside_project: 1,
      domains: [{ domain: "github.com", lookups: 2, connections: 3, new: false },
        { domain: "anthropic.com", lookups: 1, connections: 0, new: false },
        { domain: "evil.test", lookups: 1, connections: 0, new: true }],
      timeline: [4, 0, 1],
      command_timeline: [0, 2, 0],
      timeline_start_ns: 0,
      timeline_bucket_ns: 20 * MINUTE_NS,
    }),
  });
}

function renderSummary(read: AgentSessionTotals | undefined, onAlerts = vi.fn()) {
  render(
    <SessionSummary totals={read} agent="claude-code 2.1.0" project="/Users/dev/app" firstNs={0} lastNs={42 * MINUTE_NS}
      onAlerts={onAlerts} />,
  );
  return onAlerts;
}

function section(name: string): HTMLElement {
  return screen.getByRole("region", { name });
}

// spec:web-ui/agent-session-summary-tab/the-summary-describes-the-session
describe("SessionSummary", () => {
  it("leads with the agent, its project, how long it ran, its prompts and its commands", () => {
    renderSummary(busy());
    const glance = section("At a glance");
    for (const fact of ["claude-code 2.1.0", "/Users/dev/app", "Ran for 42 min", "5 prompts", "31 commands"]) {
      expect(within(glance).getByText(fact)).toBeVisible();
    }
  });

  it("lists the tools it called and the programs it ran most", () => {
    renderSummary(busy());
    const used = section("What it used");
    expect(within(used).getByText("12 tool calls")).toBeVisible();
    expect(within(within(used).getByRole("list", { name: "Tool calls by tool" })).getAllByRole("listitem").map((li) => li.textContent))
      .toEqual(["Bash8", "Edit4"]);
    expect(within(within(used).getByRole("list", { name: "Programs by processes run" })).getAllByRole("listitem")
      .map((li) => li.textContent)).toEqual(["git7", "go3"]);
  });

  it("says what it did to files, by directory relative to the project, and what it touched outside it", () => {
    renderSummary(busy());
    const touched = section("What it touched");
    expect(within(touched).getByText("9 file events across 5 paths: 2 created, 6 modified, 1 deleted.")).toBeVisible();
    expect(within(touched).getByText("1 path outside this project.")).toBeVisible();
    expect(within(within(touched).getByRole("list", { name: "File events by directory" })).getAllByRole("listitem")
      .map((li) => li.textContent)).toEqual(["./src6", "/etc2"]);
    expect(within(touched).getByText("The busiest directories are listed.")).toBeVisible();
  });

  it("groups where it talked to by organization and marks a domain new to the host", () => {
    renderSummary(busy());
    const talked = section("Where it talked to");
    expect(within(talked).getByText("4 DNS lookups, 3 connections, 1 domain this host had not looked up before.")).toBeVisible();
    expect(within(talked).getAllByRole("heading", { level: 3 }).map((h) => h.textContent))
      .toEqual(["Model APIs", "Git hosts", "Everything else"]);
    expect(within(within(talked).getByRole("list", { name: "Git hosts" })).getByRole("listitem"))
      .toHaveTextContent("github.com2 lookups, 3 connections");
    const fresh = within(within(talked).getByRole("list", { name: "Everything else" })).getByRole("listitem");
    expect(within(fresh).getByText("New to this host")).toBeVisible();
    expect(within(within(talked).getByRole("list", { name: "Git hosts" })).queryByText("New to this host")).toBeNull();
  });

  it("says what it took in tokens and how long it ran in each mode, bypass flagged", () => {
    renderSummary(busy());
    const effort = section("Effort");
    expect(within(effort).getByText(/^50k tokens: 40 in, 48k through the prompt cache, 2k out, on claude-opus-5-5\.$/)).toBeVisible();
    expect(within(effort).getByText("Bypass permissions")).toHaveClass("badge--high");
    expect(within(effort).getByText("4 replies in 10 min")).toBeVisible();
  });

  it("draws its commands and events over the session's span", () => {
    renderSummary(busy());
    const timeline = section("Timeline");
    expect(within(timeline).getByRole("img", { name: "Commands started: 2 across the session, busiest bucket 2" })).toBeVisible();
    const events = within(timeline).getByRole("img", { name: "File, network and DNS events: 5 across the session, busiest bucket 4" });
    expect([...events.children].map((bar) => (bar as HTMLElement).style.height)).toEqual(["100%", "0%", "25%"]);
    // The axis ends where the digest's span does, which reaches past the session's last sighting (42 min) to its last event.
    expect(within(timeline).getByText(formatNs(60 * MINUTE_NS))).toBeVisible();
    expect(within(timeline).queryByText(formatNs(42 * MINUTE_NS))).toBeNull();
  });

  it("counts its alerts and links to where they are shown, without leading with them", () => {
    const onAlerts = renderSummary(busy());
    const alerts = section("Alerts");
    expect(within(alerts).getByText(/2 alerts were raised in this session/)).toBeVisible();
    fireEvent.click(within(alerts).getByRole("button", { name: "See them in Commands" }));
    expect(onAlerts).toHaveBeenCalledOnce();
    const headings = screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent);
    expect(headings[headings.length - 1]).toBe("Alerts");
  });

  // spec:web-ui/agent-session-summary-tab/an-empty-session-reads-as-empty
  it("says plainly what an empty session did not do", () => {
    renderSummary(totals());
    expect(within(section("At a glance")).getByText("0 prompts")).toBeVisible();
    expect(within(section("What it used")).getByText("No tool calls were recorded.")).toBeVisible();
    expect(within(section("What it used")).getByText("Its commands ran no programs.")).toBeVisible();
    expect(within(section("What it touched")).getByText("No file activity was recorded.")).toBeVisible();
    expect(within(section("Where it talked to")).getByText("No network activity was recorded.")).toBeVisible();
    expect(within(section("Effort")).getByText("No token usage was recorded.")).toBeVisible();
    expect(within(section("Timeline")).getByText("Nothing was recorded over time.")).toBeVisible();
    expect(within(section("Alerts")).getByText("No alerts were raised in this session.")).toBeVisible();
  });

  it("says nothing was outside a project it touched only inside", () => {
    const inside = busy();
    renderSummary({ ...inside, digest: { ...digest(), ...inside.digest, outside_project: 0 } });
    expect(within(section("What it touched")).getByText("Nothing outside this project.")).toBeVisible();
  });

  it("says when the summary could not be read", () => {
    renderSummary(undefined);
    expect(screen.getByText("The session's summary could not be read.")).toBeVisible();
  });
});
