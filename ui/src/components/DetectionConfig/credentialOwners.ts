import type { CredentialKind, CredentialOwner, ProtectedCredential } from "../../api";

// CREDENTIAL_KINDS names each kind of credential as the settings page and the alert say it, in the order the page offers them.
export const CREDENTIAL_KINDS: ReadonlyMap<CredentialKind, string> = new Map<CredentialKind, string>([
  ["ssh_private_key", "SSH private keys"],
  ["aws_credentials", "AWS credentials"],
  ["github_cli_token", "GitHub CLI token"],
  ["netrc", "Logins in .netrc"],
  ["docker_config", "Docker registry logins"],
  ["kube_config", "Kubernetes credentials"],
]);

const TEAM_ID = /^[A-Z0-9]{10}$/;

// describeOwner says which tool an owner is, in the words the page shows: an Apple tool by its identifier, every tool a developer team
// signs, or one tool of a team.
export function describeOwner(owner: CredentialOwner): string {
  if (owner.team_id === undefined) return `Apple's ${owner.signing_id ?? ""}`;
  if (owner.signing_id === undefined) return `Any tool signed by team ${owner.team_id}`;
  return `${owner.signing_id} (team ${owner.team_id})`;
}

// formatOwners writes owners in the form parseOwners reads, so an entry being edited shows its tools as they were entered.
export function formatOwners(owners: readonly CredentialOwner[]): string {
  return owners
    .map((o) => {
      if (o.team_id === undefined) return o.signing_id ?? "";
      if (o.signing_id === undefined) return o.team_id;
      return `${o.team_id}:${o.signing_id}`;
    })
    .join(", ");
}

// parseOwners reads the tools typed for a credential, separated by commas: a ten-character team identifier is every tool that team
// signs, TEAMID:identifier is one tool of the team, and anything else is an Apple tool's signing identifier. Validation is the
// server's; this only splits what was typed.
export function parseOwners(text: string): CredentialOwner[] {
  return text
    .split(",")
    .map((t) => t.trim())
    .filter((t) => t !== "")
    .map((t) => {
      const colon = t.indexOf(":");
      if (colon > 0) return { team_id: t.slice(0, colon), signing_id: t.slice(colon + 1) };
      return TEAM_ID.test(t) ? { team_id: t } : { signing_id: t };
    });
}

// credentialKey identifies a credential for React keys and for comparing the draft with the stored policy. The default flag is left
// out: it records where an entry came from, not what the guard does.
export function credentialKey(c: ProtectedCredential): string {
  return JSON.stringify([c.kind, c.path, c.match, c.owners.map((o) => [o.team_id ?? "", o.signing_id ?? ""])]);
}
