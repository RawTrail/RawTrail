// socketDaemon names the daemon behind a Unix-domain socket an AI agent session connected to (issue #1355), from the socket's
// well-known path. The connect is the session's request; the daemon serves it on its own, so naming it says where work was handed
// off, not what the daemon then did. A path no entry matches is shown without a name.
const DAEMONS: { name: string; path: RegExp }[] = [
  // Docker Desktop's and the engine's socket, and the runtimes that stand in for it on a Mac: Colima, OrbStack, Rancher Desktop.
  {
    name: "Docker",
    path: /^(?:\/private)?\/var\/run\/docker\.sock$|^\/Users\/[^/]+\/(?:\.docker\/run|\.colima\/[^/]+|\.orbstack\/run|\.rd)\/docker\.sock$/,
  },
  { name: "Podman", path: /\/podman(?:-machine-default)?(?:-api)?\.sock$/ },
  // launchd's per-user ssh-agent socket, SSH_AUTH_SOCK in a login session, and 1Password's agent.
  { name: "ssh-agent", path: /^\/private\/tmp\/com\.apple\.launchd\.[^/]+\/Listeners$/ },
  { name: "1Password SSH agent", path: /\/2BUA8C4S2C\.com\.1password\/t\/agent\.sock$/ },
  { name: "PostgreSQL", path: /\/\.s\.PGSQL\.\d+$/ },
  { name: "MySQL", path: /\/mysqlx?\.sock$/ },
  { name: "Redis", path: /\/redis(?:-server)?\.sock$/ },
  // Name resolution: every lookup a process makes through the system resolver connects here.
  { name: "mDNSResponder (DNS)", path: /^(?:\/private)?\/var\/run\/mDNSResponder$/ },
];

export function socketDaemon(path: string): string | undefined {
  return DAEMONS.find((d) => d.path.test(path))?.name;
}
