// SESSION_ID_PREFIX is how much of a session id a table shows; the full id goes in the cell's title.
const SESSION_ID_PREFIX = 8;

// shortSessionId is the part of a session id a table shows, the same wherever a session is listed.
export function shortSessionId(sessionId: string): string {
  return sessionId.slice(0, SESSION_ID_PREFIX);
}

// sessionPath is an AI agent session's own view, under the host that ran it.
export function sessionPath(hostId: string, sessionId: string): string {
  return `/agent-sessions/${encodeURIComponent(hostId)}/${encodeURIComponent(sessionId)}`;
}
