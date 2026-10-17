import { describe, expect, it } from "vitest";

import { socketDaemon } from "./socketDaemon";

describe("socketDaemon", () => {
  // spec:web-ui/socket-daemon-names/a-connect-to-a-well-known-socket-names-its-daemon
  it.each([
    ["/var/run/docker.sock", "Docker"],
    ["/private/var/run/docker.sock", "Docker"],
    ["/Users/v/.docker/run/docker.sock", "Docker"],
    ["/Users/v/.colima/default/docker.sock", "Docker"],
    ["/Users/v/.orbstack/run/docker.sock", "Docker"],
    ["/Users/v/.rd/docker.sock", "Docker"],
    ["/Users/v/.local/share/containers/podman/machine/podman.sock", "Podman"],
    ["/private/tmp/com.apple.launchd.iIlTeYzViU/Listeners", "ssh-agent"],
    ["/Users/v/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock", "1Password SSH agent"],
    ["/tmp/.s.PGSQL.5432", "PostgreSQL"],
    ["/tmp/mysql.sock", "MySQL"],
    ["/opt/homebrew/var/run/redis.sock", "Redis"],
    ["/private/var/run/mDNSResponder", "mDNSResponder (DNS)"],
  ])("names %s as %s", (path, name) => {
    expect(socketDaemon(path)).toBe(name);
  });

  it.each([
    "/private/tmp/qa1355.sock",
    "/var/run/docker.sock.bak",
    "/Users/v/project/docker.sock",
    "/private/tmp/com.apple.launchd.x/Listeners/extra",
    "/tmp/.s.PGSQL.5432.lock",
  ])("names no daemon for %s", (path) => {
    expect(socketDaemon(path)).toBeUndefined();
  });
});
