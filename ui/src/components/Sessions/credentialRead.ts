import type { Alert } from "../../types";

// CREDENTIAL_READ_RULE is the rule that alerts an AI agent's process reading a credential file that is not its tool's (issue #1432).
export const CREDENTIAL_READ_RULE = "agent_credential_read";

// The rule describes each read as "An AI agent's <path> tried to read <credential> (<file>)", or "was stopped from reading" in protect
// mode, where <credential> is in plain words ("your AWS credentials") and <file> is an absolute path, which may hold parentheses.
const READ = /^An AI agent's .+? (tried to read|was stopped from reading) (.+?) \(\/.*\)$/;

// CredentialRead is what an agent did with a credential, in the words a command's badge says it.
export interface CredentialRead {
  readonly alertId: number;
  readonly text: string;
  readonly stopped: boolean;
}

// credentialReads picks the credential guard's alerts from a command's and says, for each, what the agent did: "Tried to read your
// AWS credentials", or "Stopped from reading your SSH private key". An alert from another rule, or a description in a form this build
// does not know, is left to the command's alert count.
export function credentialReads(alerts: readonly Alert[]): CredentialRead[] {
  const out: CredentialRead[] = [];
  for (const a of alerts) {
    if (a.rule_id !== CREDENTIAL_READ_RULE) continue;
    const m = READ.exec(a.description);
    if (m === null) continue;
    const stopped = m[1] === "was stopped from reading";
    out.push({ alertId: a.id, text: `${stopped ? "Stopped from reading" : "Tried to read"} ${m[2]}`, stopped });
  }
  return out;
}
