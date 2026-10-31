import { describe, it, expect } from "vitest";
import { credentialReads } from "./credentialRead";
import type { Alert } from "../../types";

function alert(id: number, ruleId: string, description: string): Alert {
  return {
    id,
    host_id: "h",
    rule_id: ruleId,
    source: "detection",
    severity: "high",
    title: "AI agent read a credential file",
    description,
    process_id: 7,
    status: "open",
    created_at: "2026-10-30T12:00:00Z",
    updated_at: "2026-10-30T12:00:00Z",
  };
}

describe("credentialReads", () => {
  // spec:web-ui/a-command-that-read-a-credential-says-so/a-command-names-the-credential-it-read
  it("says what the agent did with each credential, in the alert's own words, in detect and protect mode", () => {
    expect(
      credentialReads([
        alert(1, "agent_credential_read", "An AI agent's /bin/cat tried to read your AWS credentials (/Users/a/.aws/credentials)"),
        alert(2, "agent_credential_read", "An AI agent's /usr/bin/python3 was stopped from reading your SSH private key (/Users/a/.ssh/id_rsa)"),
        alert(3, "agent_credential_read", "An AI agent's /bin/cat tried to read the logins in your .netrc (/Users/a/work (old)/.netrc)"),
      ]),
    ).toEqual([
      { alertId: 1, text: "Tried to read your AWS credentials", stopped: false },
      { alertId: 2, text: "Stopped from reading your SSH private key", stopped: true },
      { alertId: 3, text: "Tried to read the logins in your .netrc", stopped: false },
    ]);
  });

  it("leaves other rules' alerts, and a description in a form it does not know, to the alert count", () => {
    expect(
      credentialReads([
        alert(1, "suspicious_exec", "An AI agent's /bin/cat tried to read your AWS credentials (/x)"),
        alert(2, "agent_credential_read", "Something a newer server says"),
      ]),
    ).toEqual([]);
  });
});
