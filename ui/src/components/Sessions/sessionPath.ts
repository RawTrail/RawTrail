// sessionPath is an AI agent session's own view, under the host that ran it.
export function sessionPath(hostId: string, sessionId: string): string {
  return `/agent-sessions/${encodeURIComponent(hostId)}/${encodeURIComponent(sessionId)}`;
}
