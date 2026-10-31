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

// Starting only where a run of address characters starts keeps the match linear in the text's length.
const EMAIL_LIKE = /(?<![\w.+-])[\w.+-]+@[\w.-]+/g;
// A home folder and the user it names: macOS, Linux, root's, and Windows. Both file systems ignore case, and a Windows profile folder
// may hold spaces, so its name runs to the next backslash.
const HOME = /(?:\/Users|\/home)\/[^/\s]+|\/var\/root|[A-Za-z]:\\Users\\[^\\]+/gi;
// An address is bounded by what cannot continue it, not by a word boundary, so one inside a name (backup_10.0.0.5) is found too.
// eslint-disable-next-line security/detect-unsafe-regex -- every repetition is bounded ({1,3}, {3}), so matching is linear
const IPV4 = /(?<![\d.])(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?(?!\d)/g;
// Two colons or more between hex groups: an IPv6 address, compressed or not. It may follow an underscore or a digit (backup_fd12::1),
// but not a letter, so a path like Foo::bar is left alone. A clock time with seconds matches too, which hides a time rather than leak
// an address.
// eslint-disable-next-line security/detect-unsafe-regex -- every repetition is bounded ({0,4}, {2,7}), so matching is linear
const IPV6 = /(?<![\p{L}:])[0-9A-Fa-f]{0,4}(?::[0-9A-Fa-f]{0,4}){2,7}(?![\p{L}:])/gu;
// eslint-disable-next-line security/detect-non-literal-regexp -- built once from the fixed suffix list, each escaped
const INTERNAL_HOST = new RegExp(String.raw`(?<![\w.-])[\w.-]+\.(?:${INTERNAL_SUFFIXES.map(escape).join("|")})\b`, "gi");
// eslint-disable-next-line security/detect-non-literal-regexp -- built once from the two address patterns above
const ADDRESS = new RegExp(`^(?:${IPV4.source}|${IPV6.source})$`, "u");

function escape(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// names is every name to look for: each given one, however short, and a hostname's first label (mbp from mbp.example.com), longest
// first so a name is never cut short by one it contains. Matching whole words keeps a short name from taking longer words with it.
function names(identities: Identities): string[] {
  const all = new Set<string>();
  for (const u of identities.usernames) all.add(u.toLowerCase());
  for (const h of identities.hostnames) {
    const host = h.toLowerCase().replace(/\.$/, "");
    all.add(host);
    all.add(host.split(".")[0]);
  }
  all.delete("");
  return [...all].sort((a, b) => b.length - a.length);
}

// redact removes from text everything that names a person or a machine. Home folders become ~; everything else becomes HIDDEN.
export function redact(text: string, identities: Identities): string {
  let out = text.replace(EMAIL_LIKE, HIDDEN).replace(HOME, "~").replace(IPV4, HIDDEN).replace(IPV6, HIDDEN).replace(INTERNAL_HOST, HIDDEN);
  for (const name of names(identities)) {
    // A name is matched whole, not inside a longer word: "ann" leaves "annotate" alone.
    // eslint-disable-next-line security/detect-non-literal-regexp -- the name is escaped, so it matches only itself
    out = out.replace(new RegExp(String.raw`(?<!\w)${escape(name)}(?!\w)`, "gi"), HIDDEN);
  }
  // A removal followed by another, with only space between, is one removal.
  return out.replace(/…\s*(?=…)/g, "").replace(/\s+/g, " ").trim();
}

// isInternalDomain says whether a domain belongs inside a network: an internal suffix, or a domain this session's host is named under,
// usually the owner's employer. For laptop.eng.example.com that is eng.example.com and example.com, since the server folds a
// destination to its registrable domain. A two-letter-and-two-letter tail like co.uk hides every domain under it, which errs toward
// hiding.
export function isInternalDomain(domain: string, identities: Identities): boolean {
  const d = domain.toLowerCase().replace(/\.$/, "");
  if (INTERNAL_SUFFIXES.some((s) => d === s || d.endsWith(`.${s}`))) return true;
  return identities.hostnames.some((h) => {
    const labels = h.toLowerCase().replace(/\.$/, "").split(".");
    for (let i = 1; i < labels.length - 1; i++) {
      const parent = labels.slice(i).join(".");
      if (d === parent || d.endsWith(`.${parent}`)) return true;
    }
    return false;
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
    for (const m of p.matchAll(/(?:\/Users\/|\/home\/)([^/\s]+)|[A-Za-z]:\\Users\\([^\\]+)/gi)) out.push(m[1] || m[2]);
  }
  return out;
}
