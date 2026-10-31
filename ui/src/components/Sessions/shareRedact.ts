// Redaction for a session shared as an image (issue #1430). A shared card is built from counts, program names and domains, and every
// string that reaches it passes through redact, which removes what names a person or a machine: usernames, hostnames, IP addresses,
// email-like user@host pairs, and home folders, which become ~.

// Identities are the names this session is known to carry: its owner's usernames (from a home path, say) and its host's names.
// Everything else redact removes by shape.
export interface Identities {
  readonly usernames: readonly string[];
  readonly hostnames: readonly string[];
}

// HIDDEN stands where something was removed, so a sentence still reads as one.
export const HIDDEN = "…";

// INTERNAL_SUFFIXES are the name suffixes that only mean something inside a network, never on the public internet.
export const INTERNAL_SUFFIXES: readonly string[] = ["local", "localdomain", "lan", "internal", "intranet", "corp", "home.arpa"];

const EMAIL_LIKE = /[\w.+-]+@[\w.-]+/g;
// A home folder and the user it names: macOS, Linux, root's, and Windows.
const HOME = /(?:\/Users|\/home)\/[^/\s]+|\/var\/root|[A-Za-z]:\\Users\\[^\\\s]+/g;
// eslint-disable-next-line security/detect-unsafe-regex -- every repetition is bounded ({1,3}, {3}), so matching is linear
const IPV4 = /\b(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?\b/g;
// Two colons or more between hex groups: an IPv6 address, compressed or not. A clock time with seconds matches too, which hides a
// time rather than leak an address.
// eslint-disable-next-line security/detect-unsafe-regex -- every repetition is bounded ({0,4}, {2,7}), so matching is linear
const IPV6 = /(?<![\w:])[0-9A-Fa-f]{0,4}(?::[0-9A-Fa-f]{0,4}){2,7}(?![\w:])/g;
// eslint-disable-next-line security/detect-non-literal-regexp -- built once from the fixed suffix list, each escaped
const INTERNAL_HOST = new RegExp(`[\\w.-]+\\.(?:${INTERNAL_SUFFIXES.map(escape).join("|")})\\b`, "gi");
// eslint-disable-next-line security/detect-non-literal-regexp -- built once from the two address patterns above
const ADDRESS = new RegExp(`^(?:${IPV4.source}|${IPV6.source})$`);
// A name shorter than this is not looked for by itself: too many ordinary words would go with it.
const MIN_NAME = 3;

function escape(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// names is every name to look for: each given one, and a hostname's first label (mbp from mbp.example.com), longest first so a name
// is never cut short by one it contains.
function names(identities: Identities): string[] {
  const all = new Set<string>();
  for (const u of identities.usernames) if (u.length >= MIN_NAME) all.add(u.toLowerCase());
  for (const h of identities.hostnames) {
    const host = h.toLowerCase().replace(/\.$/, "");
    if (host.length >= MIN_NAME) all.add(host);
    const label = host.split(".")[0];
    if (label.length >= MIN_NAME) all.add(label);
  }
  return [...all].sort((a, b) => b.length - a.length);
}

// redact removes from text everything that names a person or a machine. Home folders become ~; everything else becomes HIDDEN.
export function redact(text: string, identities: Identities): string {
  let out = text.replace(EMAIL_LIKE, HIDDEN).replace(HOME, "~").replace(IPV4, HIDDEN).replace(IPV6, HIDDEN).replace(INTERNAL_HOST, HIDDEN);
  for (const name of names(identities)) {
    // A name is matched whole, not inside a longer word: "ann" leaves "annotate" alone.
    // eslint-disable-next-line security/detect-non-literal-regexp -- the name is escaped, so it matches only itself
    out = out.replace(new RegExp(`(?<![\\w])${escape(name)}(?![\\w])`, "gi"), HIDDEN);
  }
  // A removal followed by another, with only space between, is one removal.
  return out.replace(/…\s*(?=…)/g, "").replace(/\s+/g, " ").trim();
}

// isInternalDomain says whether a domain belongs inside a network: an internal suffix, or the domain this session's host is named
// under (example.com for mbp.example.com), which is usually the owner's employer.
export function isInternalDomain(domain: string, identities: Identities): boolean {
  const d = domain.toLowerCase().replace(/\.$/, "");
  if (INTERNAL_SUFFIXES.some((s) => d === s || d.endsWith(`.${s}`))) return true;
  return identities.hostnames.some((h) => {
    const parent = h.toLowerCase().replace(/\.$/, "").split(".").slice(1).join(".");
    return parent.includes(".") && (d === parent || d.endsWith(`.${parent}`));
  });
}

// isAddress says whether a domain is an IP address written where a name would be.
export function isAddress(domain: string): boolean {
  return ADDRESS.test(domain) || /^\[.*\]$/.test(domain);
}

// homeUsernames are the users a path's home folder names: /Users/ann/src gives ann.
export function homeUsernames(...paths: readonly (string | undefined)[]): string[] {
  const out: string[] = [];
  for (const p of paths) {
    if (!p) continue;
    for (const m of p.matchAll(/(?:\/Users\/|\/home\/|[A-Za-z]:\\Users\\)([^/\\\s]+)/g)) out.push(m[1]);
  }
  return out;
}
