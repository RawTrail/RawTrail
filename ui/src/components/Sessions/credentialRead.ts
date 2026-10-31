import type { Alert } from "../../types";

// CREDENTIAL_READ_RULE is the rule that alerts an AI agent's process reading a credential file that is not its tool's (issue #1432).
export const CREDENTIAL_READ_RULE = "agent_credential_read";

// CREDENTIAL_WORDS is each credential as the rule names it (agentCredentialWords in server/rules/internal/catalog), and the words it
// uses for a kind it does not know.
const CREDENTIAL_WORDS: ReadonlySet<string> = new Set([
  "your SSH private key",
  "your AWS credentials",
  "your GitHub CLI token",
  "the logins in your .netrc",
  "your Docker registry logins",
  "your Kubernetes credentials",
  "a credential file",
]);

// The rule describes each read as "An AI agent's <opener> tried to read <credential> (<file>)", or "was stopped from reading" in
// protect mode. The opener is a path anyone can name a binary, so it may itself hold the words that follow it: the match is taken from
// the end, the last verdict before the file, and its credential must be one the rule names, which the opener's path cannot change.
const READ = /^An AI agent's .+ (tried to read|was stopped from reading) (.+?) \(\/.*\)$/;

// CredentialRead is what an agent did with a credential, in the words a command's badge says it, and who wrote the rule, whose credit
// every surface showing the alert carries.
export interface CredentialRead {
  readonly alertId: number;
  readonly text: string;
  readonly stopped: boolean;
  readonly origin?: string;
}

// credentialReads picks the credential guard's alerts from a command's and says, for each, what the agent did: "Tried to read your
// AWS credentials", or "Stopped from reading your SSH private key". An alert from another rule, or a description in a form this build
// does not know, is left to the command's alert count.
export function credentialReads(alerts: readonly Alert[]): CredentialRead[] {
  const out: CredentialRead[] = [];
  for (const a of alerts) {
    if (a.rule_id !== CREDENTIAL_READ_RULE) continue;
    const m = READ.exec(a.description);
    if (m === null || !CREDENTIAL_WORDS.has(m[2])) continue;
    const stopped = m[1] === "was stopped from reading";
    out.push({
      alertId: a.id,
      text: `${stopped ? "Stopped from reading" : "Tried to read"} ${m[2]}`,
      stopped,
      ...(a.origin ? { origin: a.origin } : {}),
    });
  }
  return out;
}
