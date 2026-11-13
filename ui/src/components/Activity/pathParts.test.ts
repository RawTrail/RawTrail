import { describe, expect, it } from "vitest";
import { splitPath } from "./pathParts";

describe("splitPath", () => {
  it.each([
    ["a home credential", "/Users/dev/.ssh/id_rsa", "/Users/dev", "/.ssh/id_rsa"],
    ["a launch agent", "/Users/dev/Library/LaunchAgents/x.plist", "/Users/dev/Library", "/LaunchAgents/x.plist"],
    ["three segments", "/a/b/c", "/a", "/b/c"],
    ["a rolled-up directory", "/r/node_modules/pkg/", "/r", "/node_modules/pkg/"],
    ["a relative path", "src/app/main.go", "src", "/app/main.go"],
  ])("keeps the parent and the name of %s as its tail", (_, path, head, tail) => {
    expect(splitPath(path)).toEqual({ head, tail });
  });

  it.each([
    ["a long name under /tmp", "/tmp/claude-tool-output-0123456789abcdef.json", "/tmp", "/claude-tool-output-0123456789abcdef.json"],
    ["a socket", "/tmp/x.sock", "/tmp", "/x.sock"],
    ["a directory with a trailing slash", "/private/tmp/", "/private", "/tmp/"],
    ["a relative pair", "src/main.go", "src", "/main.go"],
  ])("keeps the first segment of %s, a two-segment path, as its head", (_, path, head, tail) => {
    expect(splitPath(path)).toEqual({ head, tail });
  });

  it.each([
    ["the root", "/"],
    ["one segment", "/tmp"],
    ["one segment and a trailing slash", "/tmp/"],
    ["a bare name", "main.go"],
    ["an empty path", ""],
  ])("shows %s whole", (_, path) => {
    expect(splitPath(path)).toEqual({ head: "", tail: path });
  });

  it("always rejoins to the whole path", () => {
    for (const path of ["/Users/dev/.ssh/id_rsa", "/a/b/c/d/e/", "/x", "a/b/c", "/var/run/docker.sock"]) {
      const { head, tail } = splitPath(path);
      expect(head + tail).toBe(path);
    }
  });
});
