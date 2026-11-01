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
// one), and how many agent sessions it has recorded (null when they could not be read). An operator who may not read sessions has no
// session step: the checklist cannot tell whether it is done.
export interface SetupInputs {
  readonly host: HostSummary | null;
  readonly health: HostHealth | null;
  readonly sessions: number | null;
  readonly canReadSessions: boolean;
}

const EXTENSIONS_PANE = "System Settings > General > Login Items & Extensions";
const FULL_DISK_ACCESS_PANE = "System Settings > Privacy & Security > Full Disk Access";
// The agent's reasons, from agent/health: an extension that has never connected, a component macOS refused Full Disk Access, and a
// capture provider that is running (a provider turned off reports healthy too, under its own reason).
const NEVER_CONNECTED = "never_connected";
const FULL_DISK_ACCESS_MISSING = "full_disk_access_missing";
const ACTIVATED = "activated";

function healthy(c: ComponentHealth | undefined): boolean {
  return c?.status === "healthy";
}

// approved says an extension was allowed: it has connected at least once, whatever it reports now. An allowed network extension with no
// provider running is a filtering problem, and an allowed security extension refused Full Disk Access a grant problem, each its own step.
function approved(c: ComponentHealth | undefined): boolean {
  return c !== undefined && c.reason !== NEVER_CONNECTED;
}

function capturing(c: ComponentHealth | undefined): boolean {
  return c?.reason === ACTIVATED;
}

// grantState reads a Full Disk Access grant from its component: refused is red, healthy is done, and anything else is not known yet.
function grantState(c: ComponentHealth | undefined): StepState {
  if (c?.reason === FULL_DISK_ACCESS_MISSING) return "missing";
  return healthy(c) ? "done" : "waiting";
}

// setupSteps walks the approvals a Mac needs, in the order macOS asks for them (issue #1436), from what the agent last reported.
export function setupSteps({ host, health, sessions, canReadSessions }: SetupInputs): SetupStep[] {
  const components = new Map((health?.components ?? []).map((c) => [c.type, c]));
  const security = components.get("endpoint_security_extension");
  const steps: SetupStep[] = [
    {
      id: "enrolled",
      title: "Install the agent",
      state: host ? "done" : "waiting",
      instruction: "Install the RawTrail agent package on this Mac. It enrolls with this server on its own.",
    },
    {
      id: "security_extension",
      title: "Allow the security extension",
      state: approved(security) ? "done" : "waiting",
      instruction: `In ${EXTENSIONS_PANE}, open Endpoint Security Extensions and turn on RawTrail.`,
    },
    {
      id: "network_extension",
      title: "Allow the network extension",
      state: approved(components.get("network_extension")) ? "done" : "waiting",
      instruction: `In ${EXTENSIONS_PANE}, open Network Extensions and turn on RawTrail.`,
    },
    {
      id: "network_filter",
      title: "Allow network filtering",
      state: capturing(components.get("content_filter")) && capturing(components.get("dns_proxy")) ? "done" : "waiting",
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
  ];
  if (canReadSessions) {
    steps.push({
      id: "first_session",
      title: "Record the first agent session",
      state: sessions ? "done" : "waiting",
      instruction: "Now run claude in a terminal. Its session appears under Agent sessions.",
    });
  }
  return steps;
}

// latestMac is the Mac a checklist with no host named sets up: the one that reported most recently, which on a laptop is the only one.
// Hosts on other platforms have none of these approvals to make.
export function latestMac(hosts: readonly HostSummary[]): HostSummary | null {
  return hosts
    .filter((h) => h.platform === "darwin")
    .reduce<HostSummary | null>((best, h) => (best && best.last_seen_ns >= h.last_seen_ns ? best : h), null);
}
