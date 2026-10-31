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

// APPLE_PREFIX marks an Apple tool explicitly, for an identifier that would otherwise read as a team or as a team's tool.
const APPLE_PREFIX = "apple:";

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
      if (o.team_id === undefined) {
        const id = o.signing_id ?? "";
        return TEAM_ID.test(id) || id.includes(":") ? APPLE_PREFIX + id : id;
      }
      if (o.signing_id === undefined) return o.team_id;
      return `${o.team_id}:${o.signing_id}`;
    })
    .join(", ");
}

// parseOwners reads the tools typed for a credential, separated by commas: a ten-character team identifier is every tool that team
// signs, TEAMID:identifier is one tool of the team, apple:identifier is an Apple tool whatever its identifier looks like, and anything
// else is an Apple tool's signing identifier. Validation is the server's; this only splits what was typed.
export function parseOwners(text: string): CredentialOwner[] {
  return text
    .split(",")
    .map((t) => t.trim())
    .filter((t) => t !== "")
    .map((t) => {
      if (t.startsWith(APPLE_PREFIX)) return { signing_id: t.slice(APPLE_PREFIX.length) };
      const colon = t.indexOf(":");
      if (colon > 0) return { team_id: t.slice(0, colon), signing_id: t.slice(colon + 1) };
      return TEAM_ID.test(t) ? { team_id: t } : { signing_id: t };
    });
}

// ownersProblem names what is wrong with the tools typed for a credential, or returns null. Only the form's own notation is checked: a
// colon with nothing on one side would otherwise be sent as a whole team, when one of its tools was meant.
export function ownersProblem(text: string): string | null {
  const bad = text
    .split(",")
    .map((t) => t.trim())
    .find((t) => {
      const colon = t.indexOf(":");
      return colon >= 0 && (colon === 0 || colon === t.length - 1);
    });
  return bad === undefined ? null : `"${bad}" names no tool. Write one of a team's tools as TEAMID:identifier.`;
}

// credentialKey identifies a credential for React keys and for comparing the draft with the stored policy. The default flag is left
// out: it records where an entry came from, not what the guard does.
export function credentialKey(c: ProtectedCredential): string {
  return JSON.stringify([c.kind, c.path, c.match, c.owners.map((o) => [o.team_id ?? "", o.signing_id ?? ""])]);
}
