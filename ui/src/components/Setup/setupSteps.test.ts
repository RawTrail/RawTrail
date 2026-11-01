import { describe, expect, it } from "vitest";
import type { ComponentHealth, HostHealth, HostSummary } from "../../types";
import { latestMac, setupSteps, type StepState } from "./setupSteps";

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

// ALL_DONE is the report of a Mac with every approval made.
const ALL_DONE = health(
  component("endpoint_security_extension", "healthy", "activated"),
  component("network_extension", "healthy", "activated"),
  component("content_filter", "healthy", "activated"),
  component("dns_proxy", "healthy", "activated"),
  component("agent_full_disk_access", "healthy", "full_disk_access_granted"),
);

function states(...args: Parameters<typeof setupSteps>): Record<string, StepState> {
  return Object.fromEntries(setupSteps(...args).map((s) => [s.id, s.state]));
}

describe("setupSteps", () => {
  // spec:web-ui/the-first-run-checklist-walks-the-approvals-from-live-health/each-approval-shows-where-it-stands
  it("lists the approvals in the order macOS asks for them, each saying where to make it", () => {
    const steps = setupSteps({ host: null, health: null, sessions: null, canReadSessions: true });
    expect(steps.map((s) => s.id)).toEqual([
      "enrolled",
      "security_extension",
      "network_extension",
      "network_filter",
      "security_extension_fda",
      "agent_fda",
      "first_event",
      "first_session",
    ]);
    expect(steps.every((s) => s.state === "waiting")).toBe(true);
    expect(steps.find((s) => s.id === "security_extension")?.instruction).toContain("Endpoint Security Extensions");
    expect(steps.find((s) => s.id === "first_session")?.instruction).toContain("run claude in a terminal");
  });

  it("marks every step done for a Mac with every approval made, an event and a session", () => {
    const all = states({ host: host({ event_count: 3 }), health: ALL_DONE, sessions: 1, canReadSessions: true });
    expect(Object.values(all).every((s) => s === "done")).toBe(true);
  });

  it("marks an enrolled Mac whose extensions are not yet approved as waiting on them", () => {
    const got = states({
      host: host(),
      health: health(
        component("endpoint_security_extension", "unhealthy", "never_connected"),
        component("network_extension", "unhealthy", "never_connected"),
      ),
      sessions: 0,
      canReadSessions: true,
    });
    expect(got).toMatchObject({
      enrolled: "done",
      security_extension: "waiting",
      network_extension: "waiting",
      network_filter: "waiting",
      security_extension_fda: "waiting",
      agent_fda: "waiting",
      first_event: "waiting",
      first_session: "waiting",
    });
  });

  it("waits on network filtering until both the content filter and the DNS proxy are capturing", () => {
    const proxyStopped = health(component("content_filter", "healthy", "activated"), component("dns_proxy", "unhealthy", "provider_stopped"));
    expect(states({ host: host(), health: proxyStopped, sessions: 0, canReadSessions: true }).network_filter).toBe("waiting");
    const filterStopped = health(component("content_filter", "unhealthy", "provider_stopped"), component("dns_proxy", "healthy", "activated"));
    expect(states({ host: host(), health: filterStopped, sessions: 0, canReadSessions: true }).network_filter).toBe("waiting");
    // A provider someone turned off reports healthy, under its own reason: it is not capturing.
    const proxyOff = health(component("content_filter", "healthy", "activated"), component("dns_proxy", "healthy", "provider_disabled"));
    expect(states({ host: host(), health: proxyOff, sessions: 0, canReadSessions: true }).network_filter).toBe("waiting");
  });

  it("counts a network extension that connected with no provider running as approved, leaving filtering open", () => {
    for (const reason of ["awaiting_provider_status", "no_providers_running", "provider_stopped"]) {
      const got = states({ host: host(), health: health(component("network_extension", "unhealthy", reason)), sessions: 0,
        canReadSessions: true });
      expect(got, reason).toMatchObject({ network_extension: "done", network_filter: "waiting" });
    }
  });

  it("has no session step for an operator who may not read sessions", () => {
    const steps = setupSteps({ host: host({ event_count: 3 }), health: ALL_DONE, sessions: null, canReadSessions: false });
    expect(steps.map((s) => s.id)).not.toContain("first_session");
    expect(steps.every((s) => s.state === "done")).toBe(true);
  });

  // spec:web-ui/the-first-run-checklist-walks-the-approvals-from-live-health/a-refused-full-disk-access-grant-is-a-red-step
  it("shows a refused Full Disk Access grant as needing the person, for the extension and for the agent", () => {
    const refused = health(
      component("endpoint_security_extension", "unhealthy", "full_disk_access_missing"),
      component("agent_full_disk_access", "unhealthy", "full_disk_access_missing"),
    );
    const got = states({ host: host(), health: refused, sessions: 0, canReadSessions: true });
    // The extension ran and macOS refused its client, so it was approved: only its grant is open.
    expect(got).toMatchObject({ security_extension: "done", security_extension_fda: "missing", agent_fda: "missing" });
    const fda = setupSteps({ host: host(), health: refused, sessions: 0, canReadSessions: true }).find((s) => s.id === "security_extension_fda");
    expect(fda?.instruction).toContain("Privacy & Security > Full Disk Access");
  });

  it("counts an extension that connected once and lost its connection as approved", () => {
    const lost = health(component("endpoint_security_extension", "unhealthy", "connection_lost"));
    expect(states({ host: host(), health: lost, sessions: 0, canReadSessions: true })).toMatchObject({ security_extension: "done", security_extension_fda: "waiting" });
  });

  it("leaves the session step waiting when sessions could not be read", () => {
    expect(states({ host: host({ event_count: 1 }), health: ALL_DONE, sessions: null, canReadSessions: true }).first_session).toBe("waiting");
  });
});

describe("latestMac", () => {
  it("picks the Mac that reported most recently, and none from an empty list", () => {
    expect(latestMac([])).toBeNull();
    const older = host({ host_id: "old", last_seen_ns: 5 });
    const newer = host({ host_id: "new", last_seen_ns: 9 });
    expect(latestMac([older, newer])?.host_id).toBe("new");
    expect(latestMac([newer, older])?.host_id).toBe("new");
  });

  it("passes over hosts on other platforms, however recently they reported", () => {
    const mac = host({ host_id: "mac", last_seen_ns: 5 });
    const windows = host({ host_id: "win", platform: "windows", last_seen_ns: 9 });
    expect(latestMac([mac, windows])?.host_id).toBe("mac");
    expect(latestMac([windows])).toBeNull();
  });
});
