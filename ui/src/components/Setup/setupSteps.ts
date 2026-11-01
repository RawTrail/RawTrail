import type { ComponentHealth, HostHealth, HostSummary } from "../../types";

// StepState is where one approval stands: done; missing, which needs the person and is shown red; or waiting, which is either the next
// thing to do or something an earlier step still blocks.
export type StepState = "done" | "missing" | "waiting";

export interface SetupStep {
  readonly id: string;
  readonly title: string;
  readonly state: StepState;
  // instruction says where to make the approval, or what to do next.
  readonly instruction: string;
}

// SetupInputs is what the checklist reads: the Mac being set up (null until it enrolls), its last health report (null until it sends
// one), and how many agent sessions it has recorded (null when they could not be read).
export interface SetupInputs {
  readonly host: HostSummary | null;
  readonly health: HostHealth | null;
  readonly sessions: number | null;
}

const EXTENSIONS_PANE = "System Settings > General > Login Items & Extensions";
const FULL_DISK_ACCESS_PANE = "System Settings > Privacy & Security > Full Disk Access";
// The agent's reasons, from agent/health: an extension that has never connected, and a component macOS refused Full Disk Access.
const NEVER_CONNECTED = "never_connected";
const FULL_DISK_ACCESS_MISSING = "full_disk_access_missing";

function healthy(c: ComponentHealth | undefined): boolean {
  return c?.status === "healthy";
}

// grantState reads a Full Disk Access grant from its component: refused is red, healthy is done, and anything else is not known yet.
function grantState(c: ComponentHealth | undefined): StepState {
  if (c?.reason === FULL_DISK_ACCESS_MISSING) return "missing";
  return healthy(c) ? "done" : "waiting";
}

// setupSteps walks the approvals a Mac needs, in the order macOS asks for them (issue #1436), from what the agent last reported.
export function setupSteps({ host, health, sessions }: SetupInputs): SetupStep[] {
  const components = new Map((health?.components ?? []).map((c) => [c.type, c]));
  const security = components.get("endpoint_security_extension");
  return [
    {
      id: "enrolled",
      title: "Install the agent",
      state: host ? "done" : "waiting",
      instruction: "Install the RawTrail agent package on this Mac. It enrolls with this server on its own.",
    },
    {
      // An extension refused Full Disk Access was approved: it ran, and macOS stopped its client. The grant is its own step below.
      id: "security_extension",
      title: "Allow the security extension",
      state: security && security.reason !== NEVER_CONNECTED ? "done" : "waiting",
      instruction: `In ${EXTENSIONS_PANE}, open Endpoint Security Extensions and turn on RawTrail.`,
    },
    {
      id: "network_extension",
      title: "Allow the network extension",
      state: healthy(components.get("network_extension")) ? "done" : "waiting",
      instruction: `In ${EXTENSIONS_PANE}, open Network Extensions and turn on RawTrail.`,
    },
    {
      id: "network_filter",
      title: "Allow network filtering",
      state: healthy(components.get("content_filter")) && healthy(components.get("dns_proxy")) ? "done" : "waiting",
      instruction: "When macOS asks whether RawTrail may filter network content, click Allow.",
    },
    {
      id: "security_extension_fda",
      title: "Give the security extension Full Disk Access",
      state: grantState(security),
      instruction: `In ${FULL_DISK_ACCESS_PANE}, turn on RawTrail Security Extension.`,
    },
    {
      id: "agent_fda",
      title: "Give the agent Full Disk Access",
      state: grantState(components.get("agent_full_disk_access")),
      instruction: `In ${FULL_DISK_ACCESS_PANE}, click +, press Cmd+Shift+G, enter /usr/local/bin/rawtrail-agent, and turn it on.`,
    },
    {
      id: "first_event",
      title: "Receive the first event",
      state: host && host.event_count > 0 ? "done" : "waiting",
      instruction: "Events arrive within a minute of the security extension starting.",
    },
    {
      id: "first_session",
      title: "Record the first agent session",
      state: sessions ? "done" : "waiting",
      instruction: "Now run claude in a terminal. Its session appears under Agent sessions.",
    },
  ];
}

// latestHost is the Mac a checklist with no host named sets up: the one that reported most recently, which on a laptop is the only one.
export function latestHost(hosts: readonly HostSummary[]): HostSummary | null {
  return hosts.reduce<HostSummary | null>((best, h) => (best && best.last_seen_ns >= h.last_seen_ns ? best : h), null);
}
