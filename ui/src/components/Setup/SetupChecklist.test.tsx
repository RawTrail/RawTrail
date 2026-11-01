import { act, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "../../api";
import { PermissionAction, PermissionsContext } from "../../permissions-core";
import type { ComponentHealth, HostHealth, HostSummary } from "../../types";
import { POLL_MS, SetupChecklist } from "./SetupChecklist";

const host = (over: Partial<HostSummary> = {}): HostSummary => ({
  host_id: "mac",
  hostname: "mac.local",
  os_version: "26.0",
  platform: "darwin",
  event_count: 0,
  last_seen_ns: 1,
  overall_status: "unhealthy",
  ...over,
});

const component = (type: string, status: string, reason: string): ComponentHealth => ({ type, status, reason, last_transition_ns: 1 });

const health = (...components: ComponentHealth[]): HostHealth => ({
  overall_status: "unhealthy",
  reported_at_ns: 1,
  components,
  derived_components: null,
  episodes: [],
});

const ALL_DONE = health(
  component("endpoint_security_extension", "healthy", "activated"),
  component("network_extension", "healthy", "activated"),
  component("content_filter", "healthy", "activated"),
  component("dns_proxy", "healthy", "activated"),
  component("agent_full_disk_access", "healthy", "full_disk_access_granted"),
);

function renderAt(path = "/setup", permissions?: string[]) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <PermissionsContext.Provider value={permissions}>
        <SetupChecklist />
      </PermissionsContext.Provider>
    </MemoryRouter>,
  );
}

// step finds a step's row by its title.
function step(title: string): HTMLElement {
  const row = screen.getByRole("heading", { name: title }).closest("li");
  if (!row) throw new Error(`no step ${title}`);
  return row;
}

// settle lets the page's reads resolve under fake timers.
async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("SetupChecklist", () => {
  // spec:web-ui/the-first-run-checklist-walks-the-approvals-from-live-health/a-refused-full-disk-access-grant-is-a-red-step
  it("shows the Mac's refused Full Disk Access grant as a step that needs the person", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([host()]);
    vi.spyOn(api, "getHostHealth").mockResolvedValue(health(component("endpoint_security_extension", "unhealthy", "full_disk_access_missing")));
    vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [], total_matched: 0 });
    renderAt();
    await settle();

    expect(screen.getByRole("heading", { name: "Set up this Mac" })).toBeVisible();
    expect(screen.getByText("mac.local")).toBeVisible();
    const fda = step("Give the security extension Full Disk Access");
    expect(within(fda).getByText("Action needed")).toBeVisible();
    expect(fda).toHaveClass("setup-checklist__step--missing");
    expect(within(fda).getByText(/Privacy & Security > Full Disk Access/)).toBeVisible();
    expect(within(step("Allow the security extension")).getByText("Done")).toBeVisible();
    expect(within(step("Allow the network extension")).getByText("Waiting")).toBeVisible();
  });

  // spec:web-ui/the-first-run-checklist-walks-the-approvals-from-live-health/the-checklist-follows-the-mac-as-it-is-set-up
  it("turns a step done when the next read finds it made, and stops reading once every step is done", async () => {
    const listHosts = vi.spyOn(api, "listHosts").mockResolvedValue([host({ event_count: 4 })]);
    const getHealth = vi
      .spyOn(api, "getHostHealth")
      .mockResolvedValueOnce(health(...(ALL_DONE.components ?? []).filter((c) => c.type !== "agent_full_disk_access")))
      .mockResolvedValue(ALL_DONE);
    vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [], total_matched: 1 });
    renderAt();
    await settle();

    expect(within(step("Give the agent Full Disk Access")).getByText("Waiting")).toBeVisible();
    expect(screen.getByText("1 of 8 steps left. This page updates as you go.")).toBeVisible();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_MS);
    });
    expect(getHealth).toHaveBeenCalledTimes(2);
    expect(screen.getByText(/Everything is set up/)).toBeVisible();
    expect(screen.getByRole("link", { name: "Go to Agent sessions" })).toHaveAttribute("href", "/agent-sessions");
    // A done step does not repeat its instruction.
    expect(screen.queryByText(/click \+, press Cmd\+Shift\+G/)).toBeNull();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_MS * 3);
    });
    expect(listHosts).toHaveBeenCalledTimes(2);
  });

  it("sets up the Mac named by ?host= rather than the one that reported last", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([
      host({ host_id: "named", hostname: "named.local", last_seen_ns: 1 }),
      host({ host_id: "latest", hostname: "latest.local", last_seen_ns: 9 }),
    ]);
    const getHealth = vi.spyOn(api, "getHostHealth").mockResolvedValue(health());
    const sessions = vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [], total_matched: 0 });
    renderAt("/setup?host=named");
    await settle();

    expect(screen.getByText("named.local")).toBeVisible();
    expect(getHealth).toHaveBeenCalledWith("named");
    expect(sessions).toHaveBeenCalledWith({ host_id: "named" });
  });

  it("neither reads nor lists sessions for an operator who may not read them, and finishes without them", async () => {
    const listHosts = vi.spyOn(api, "listHosts").mockResolvedValue([host({ event_count: 4 })]);
    vi.spyOn(api, "getHostHealth").mockResolvedValue(ALL_DONE);
    const sessions = vi.spyOn(api, "listAgentSessions");
    renderAt("/setup", [PermissionAction.HostRead]);
    await settle();

    expect(sessions).not.toHaveBeenCalled();
    expect(screen.queryByRole("heading", { name: "Record the first agent session" })).toBeNull();
    expect(screen.getByText(/Everything is set up/)).toBeVisible();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_MS * 2);
    });
    expect(listHosts).toHaveBeenCalledTimes(1);
  });

  it("waits for a Mac to enroll when there is none, and keeps reading", async () => {
    const listHosts = vi.spyOn(api, "listHosts").mockResolvedValue([]);
    const getHealth = vi.spyOn(api, "getHostHealth");
    renderAt();
    await settle();

    expect(screen.getByText("Waiting for this Mac to enroll")).toBeVisible();
    expect(within(step("Install the agent")).getByText("Waiting")).toBeVisible();
    expect(getHealth).not.toHaveBeenCalled();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_MS);
    });
    expect(listHosts).toHaveBeenCalledTimes(2);
  });

  it("leaves health and session steps waiting when those reads fail", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([host({ event_count: 2 })]);
    vi.spyOn(api, "getHostHealth").mockRejectedValue(new Error("down"));
    vi.spyOn(api, "listAgentSessions").mockRejectedValue(new Error("down"));
    renderAt();
    await settle();

    expect(within(step("Install the agent")).getByText("Done")).toBeVisible();
    expect(within(step("Receive the first event")).getByText("Done")).toBeVisible();
    expect(within(step("Allow the security extension")).getByText("Waiting")).toBeVisible();
    expect(within(step("Record the first agent session")).getByText("Waiting")).toBeVisible();
  });

  it("says it could not read the Mac, then recovers on the next read", async () => {
    vi.spyOn(api, "listHosts").mockRejectedValueOnce(new Error("down")).mockResolvedValue([]);
    renderAt();
    await settle();

    expect(screen.getByRole("alert")).toHaveTextContent("Could not read this Mac's status. Retrying.");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_MS);
    });
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByText("Waiting for this Mac to enroll")).toBeVisible();
  });

  it("stops reading when it is closed", async () => {
    const listHosts = vi.spyOn(api, "listHosts").mockResolvedValue([]);
    const { unmount } = renderAt();
    await settle();
    unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_MS * 2);
    });
    expect(listHosts).toHaveBeenCalledTimes(1);
  });
});
