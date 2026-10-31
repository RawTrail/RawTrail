import { describe, expect, it } from "vitest";
import { HIDDEN, homeUsernames, isAddress, isInternalDomain, redact, type Identities } from "./shareRedact";

const NONE: Identities = { usernames: [], hostnames: [] };
const ANN: Identities = { usernames: ["ann"], hostnames: ["anns-mbp.example.com"] };

// spec:web-ui/a-shared-session-image-carries-no-identifying-detail/what-names-a-person-or-a-machine-is-removed
describe("redact", () => {
  it.each([
    ["a home folder becomes ~", "/Users/ann/src/app", NONE, "~/src/app"],
    ["a Linux home folder becomes ~", "cat /home/ann/.bashrc", NONE, "cat ~/.bashrc"],
    ["root's home becomes ~", "/var/root/notes", NONE, "~/notes"],
    ["a Windows home folder becomes ~", "C:\\Users\\ann\\repo", NONE, "~\\repo"],
    ["a Windows profile folder with a space becomes ~", "C:\\Users\\Ann Smith\\repo", NONE, "~\\repo"],
    ["a home folder in any case becomes ~", "c:\\users\\alice\\repo and /users/bob/x", NONE, "~\\repo and ~/x"],
    ["a user@host pair is removed", "ssh ann@build.example.com now", NONE, `ssh ${HIDDEN} now`],
    ["an IPv4 address and its port are removed", "curl 10.0.0.5:8080/x", NONE, `curl ${HIDDEN}/x`],
    ["an IPv4 address inside a name is removed", "backup_10.0.0.5", NONE, `backup_${HIDDEN}`],
    ["an IPv6 address is removed", "ping fe80::1c2:3ff:fe4a:5b6c", NONE, `ping ${HIDDEN}`],
    ["an IPv6 address inside a name is removed", "backup_fd12::1", NONE, `backup_${HIDDEN}`],
    ["a scoped name in code is not an address", "Refactor Foo::bar and std::vec", NONE, "Refactor Foo::bar and std::vec"],
    ["a scoped name of hex letters is not an address", "call add::feature", NONE, "call add::feature"],
    ["a loopback IPv6 address is removed", "listen on ::1", NONE, `listen on ${HIDDEN}`],
    ["an internal hostname is removed", "open printer.local and nas.lan", NONE, `open ${HIDDEN} and ${HIDDEN}`],
    ["a known hostname is removed", "deploy from anns-mbp.example.com", ANN, `deploy from ${HIDDEN}`],
    ["a known hostname's first label is removed", "on anns-mbp today", ANN, `on ${HIDDEN} today`],
    ["a known username is removed in any case", "ask ANN about it", ANN, `ask ${HIDDEN} about it`],
    ["a username inside a longer word stays", "annotate the tests", ANN, "annotate the tests"],
    ["runs of removed names collapse into one", "ann anns-mbp 10.0.0.1 left", ANN, `${HIDDEN} left`],
    ["plain text is unchanged", "Add retry with backoff to the webhook sender", ANN, "Add retry with backoff to the webhook sender"],
  ])("%s", (_name, text, identities, want) => {
    expect(redact(text, identities)).toBe(want);
  });

  it("removes a known name however short, as a whole word only", () => {
    expect(redact("ask al on vm about the vmware alias", { usernames: ["al"], hostnames: ["vm"] }))
      .toBe(`ask ${HIDDEN} on ${HIDDEN} about the vmware alias`);
  });
});

describe("isInternalDomain", () => {
  it.each([
    ["an internal suffix", "printer.local", NONE, true],
    ["a home network name", "router.home.arpa", NONE, true],
    ["the domain the host is named under", "wiki.example.com", ANN, true],
    ["that domain itself", "example.com", ANN, true],
    ["the registrable domain above a nested host", "example.com", { usernames: [], hostnames: ["laptop.eng.example.com"] }, true],
    ["a sibling under that registrable domain", "jira.example.com", { usernames: [], hostnames: ["laptop.eng.example.com"] }, true],
    ["a public domain", "github.com", ANN, false],
    ["a public domain when the host has no domain", "github.com", { usernames: [], hostnames: ["anns-mbp"] }, false],
  ])("%s", (_name, domain, identities, want) => {
    expect(isInternalDomain(domain, identities)).toBe(want);
  });
});

describe("isAddress", () => {
  it.each([["10.1.2.3", true], ["::1", true], ["[2001:db8::1]", true], ["github.com", false], ["v1.2.3.example.com", false]])(
    "%s", (domain, want) => {
      expect(isAddress(domain)).toBe(want);
    });
});

describe("homeUsernames", () => {
  it("names the user each home folder belongs to", () => {
    expect(homeUsernames("/Users/ann/src", undefined, "/home/bob/x", "C:\\Users\\cy\\y", "c:\\users\\Dee Ray\\z", "/etc/hosts"))
      .toEqual(["ann", "bob", "cy", "Dee Ray"]);
  });
});
