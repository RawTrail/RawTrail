import { describe, it, expect } from "vitest";
import { credentialKey, describeOwner, formatOwners, ownersProblem, parseOwners } from "./credentialOwners";

describe("parseOwners", () => {
  it("reads an Apple tool, a whole team and one tool of a team, separated by commas", () => {
    expect(parseOwners(" com.apple.ssh, 94KV3E626L ,59GAB85EFG:com.apple.git-remote-http,, ")).toEqual([
      { signing_id: "com.apple.ssh" },
      { team_id: "94KV3E626L" },
      { team_id: "59GAB85EFG", signing_id: "com.apple.git-remote-http" },
    ]);
  });

  it("reads nothing typed as no tools", () => {
    expect(parseOwners("  ")).toEqual([]);
  });

  it("leaves a lowercase or short team-like word to the server as a signing identifier", () => {
    expect(parseOwners("94kv3e626l, ABC123")).toEqual([{ signing_id: "94kv3e626l" }, { signing_id: "ABC123" }]);
  });

  it("reads apple:identifier as an Apple tool, and writes one that would read as a team that way", () => {
    expect(parseOwners("apple:ABCDE12345, apple:com.example:x")).toEqual([{ signing_id: "ABCDE12345" }, { signing_id: "com.example:x" }]);
    const owners = [{ signing_id: "ABCDE12345" }, { signing_id: "com.example:x" }, { signing_id: "com.apple.curl" }];
    expect(formatOwners(owners)).toBe("apple:ABCDE12345, apple:com.example:x, com.apple.curl");
    expect(parseOwners(formatOwners(owners))).toEqual(owners);
  });

  it("round-trips through formatOwners, so an entry being edited shows its tools as they were entered", () => {
    const owners = [{ signing_id: "com.apple.curl" }, { team_id: "VEKTX9H2N7" }, { team_id: "ABCDE12345", signing_id: "gh" }];
    expect(parseOwners(formatOwners(owners))).toEqual(owners);
  });
});

describe("describeOwner", () => {
  it("names each kind of owner in plain words", () => {
    expect(describeOwner({ signing_id: "com.apple.ssh" })).toBe("Apple's com.apple.ssh");
    expect(describeOwner({ team_id: "94KV3E626L" })).toBe("Any tool signed by team 94KV3E626L");
    expect(describeOwner({ team_id: "ABCDE12345", signing_id: "gh" })).toBe("gh (team ABCDE12345)");
  });
});

describe("credentialKey", () => {
  it("ignores the built-in flag and tells apart any change to what the guard does", () => {
    const base = { kind: "netrc" as const, path: ".netrc", match: "literal" as const, owners: [{ signing_id: "com.apple.curl" }] };
    expect(credentialKey({ ...base, default: true })).toBe(credentialKey(base));
    expect(credentialKey({ ...base, owners: [] })).not.toBe(credentialKey(base));
    expect(credentialKey({ ...base, match: "prefix" })).not.toBe(credentialKey(base));
  });
});

describe("ownersProblem", () => {
  it("names a colon with nothing on one side, and accepts the rest", () => {
    expect(ownersProblem("com.apple.ssh, 94KV3E626L:")).toContain('"94KV3E626L:" names no tool');
    expect(ownersProblem(":gh")).toContain('":gh" names no tool');
    expect(ownersProblem("com.apple.ssh, 94KV3E626L, 59GAB85EFG:com.apple.git-remote-http")).toBeNull();
    expect(ownersProblem("")).toBeNull();
  });
});
