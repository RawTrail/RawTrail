// A session's Summary (issue #1429) describes it from the summary endpoint alone: its prompts and commands, the programs it ran, the
// files it touched by directory and outside its project, and where it talked to by organization with a domain new to the host
// marked. The session is written straight into the dev databases on a fresh host, as the paging spec writes its session. The test
// removes its MySQL rows afterwards; its archive events stay, on a host nothing else names, until the archive's TTL expires them
// (a ClickHouse delete rewrites every part of the events table).

import * as crypto from "node:crypto";
import { readFile } from "node:fs/promises";
import { test, expect } from "@playwright/test";
import { signInAsAdminViaForgedSession } from "../../fixtures/auth";
import { insertClickHouse, openDB } from "../../fixtures/db";

const PROJECT = "/Users/qa/summary";
// HOSTNAME is the seeded host's name, which a shared image must never show.
const HOSTNAME = "qa-mbp";

interface Seeded {
  hostId: string;
  sessionId: string;
}

// seedSession writes two commands the agent ran, git and npm each under the agent's wrapper shell, into MySQL, and what they did
// into the ClickHouse archive.
async function seedSession(): Promise<Seeded> {
  const hostId = crypto.randomUUID();
  const sessionId = crypto.randomUUID();
  const base = 910_000_000_000 + crypto.randomInt(1_000_000) * 1_000;
  const startNs = (Date.now() - 3_600_000) * 1_000_000;
  const wrapper = (command: string) => JSON.stringify(["/bin/zsh", "-c", `source /tmp/snap.sh && eval '${command}' < /dev/null`]);
  // id, pid, ppid, path, args, fork time, tool-call group, creator row, pidversion
  const rows: unknown[][] = [
    [base, 41_000, 1, "/bin/zsh", wrapper("git status"), startNs, 41_000, null, 11],
    [base + 1, 41_001, 41_000, "/usr/bin/git", JSON.stringify(["git", "status"]), startNs + 1_000, 41_000, base, 12],
    [base + 2, 41_002, 1, "/bin/zsh", wrapper("npm install"), startNs + 60e9, 41_002, null, 13],
    [base + 3, 41_003, 41_002, "/usr/local/bin/npm", JSON.stringify(["npm", "install"]), startNs + 60e9 + 1_000, 41_002, base + 2, 14],
  ];
  const db = await openDB();
  try {
    await db.query(
      `INSERT INTO processes (id, host_id, pid, ppid, path, args, fork_time_ns, tool_call_group, parent_process_id, pidversion, uid,
         exec_time_ns, exit_time_ns, exit_reason, is_snapshot, agent_session_id, agent_name, agent_version) VALUES ` +
        rows.map(() => "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 501, ?, ?, 'event', 0, ?, 'claude-code', '2.1.302')").join(", "),
      rows.flatMap((r) => [r[0], hostId, ...r.slice(1), Number(r[5]) + 1, Number(r[5]) + 5_000_000, sessionId]),
    );
    // The host's record and its enrolled name, which the Share dialog reads to redact.
    await db.query("INSERT INTO hosts (host_id, event_count, last_seen_ns) VALUES (?, 0, ?)", [hostId, startNs + 120e9]);
    await db.query(
      `INSERT INTO enrollments (host_id, hostname, agent_version, os_version, platform, source_ip)
         VALUES (?, ?, '2.1.302', '26.0', 'darwin', '127.0.0.1')`,
      [hostId, HOSTNAME],
    );
    await db.query(
      `INSERT INTO agent_sessions (host_id, agent_session_id, agent_name, agent_version, agent_pid, cwd, first_seen_ns, first_event_id,
         last_seen_ns) VALUES (?, ?, 'claude-code', '2.1.302', 40999, ?, ?, ?, ?)`,
      [hostId, sessionId, PROJECT, startNs, `summary-${hostId}`, startNs + 120e9],
    );
  } catch (err) {
    await removeSession({ hostId, sessionId });
    throw err;
  } finally {
    await db.end();
  }
  const event = (id: string, type: string, ts: number, payload: Record<string, unknown>) => ({
    event_id: `summary-${id}-${hostId}`, host_id: hostId, timestamp_ns: ts, ingested_at_ns: ts + 1, event_type: type, platform: "darwin",
    payload: JSON.stringify(payload),
  });
  const git = { pid: 41_001, pidversion: 12 };
  const npm = { pid: 41_003, pidversion: 14 };
  await insertClickHouse("events", [
    event("p1", "agent_prompt", startNs - 1e9, { agent_session_id: sessionId, prompt_id: "turn-1", text: "check the tree" }),
    event("p2", "agent_prompt", startNs + 59e9, { agent_session_id: sessionId, prompt_id: "turn-2", text: "install" }),
    event("f1", "file_activity", startNs + 2_000, { ...git, operation: "modify", path: `${PROJECT}/src/main.go` }),
    event("f2", "file_activity", startNs + 3_000, { ...git, operation: "create", path: `${PROJECT}/src/new.go` }),
    event("f3", "file_activity", startNs + 60e9 + 2_000, { ...npm, operation: "create", path: "/Users/qa/.npm/_cacache/entry" }),
    event("d1", "dns_query", startNs + 4_000, { ...git, query_name: "api.github.com", query_type: "A" }),
    event("n1", "network_connect", startNs + 5_000, { ...git, protocol: "tcp", remote_address: "140.82.1.1", remote_port: 443,
      remote_hostname: "api.github.com" }),
    event("d2", "dns_query", startNs + 60e9 + 3_000, { ...npm, query_name: "registry.npmjs.org", query_type: "A" }),
    event("d3", "dns_query", startNs + 60e9 + 4_000, { ...npm, query_name: `${HOSTNAME}.local`, query_type: "A" }),
    // The host had looked github.com up before the session, from another process.
    event("d0", "dns_query", startNs - 600e9, { pid: 99, pidversion: 1, query_name: "github.com", query_type: "A" }),
  ]).catch(async (err: unknown) => {
    // The MySQL rows are written: remove them, since afterEach never sees a session the seed did not return.
    await removeSession({ hostId, sessionId });
    throw err;
  });
  return { hostId, sessionId };
}

async function removeSession({ hostId }: Seeded): Promise<void> {
  const db = await openDB();
  try {
    await db.query("DELETE FROM processes WHERE host_id = ?", [hostId]);
    await db.query("DELETE FROM agent_sessions WHERE host_id = ?", [hostId]);
    await db.query("DELETE FROM enrollments WHERE host_id = ?", [hostId]);
    await db.query("DELETE FROM hosts WHERE host_id = ?", [hostId]);
  } finally {
    await db.end();
  }
}

test.describe("a session's Summary", () => {
  let seeded: Seeded | undefined;
  test.beforeEach(async () => { seeded = await seedSession(); });
  test.afterEach(async () => {
    if (seeded) await removeSession(seeded);
    seeded = undefined;
  });

  // spec:web-ui/agent-session-summary-tab/the-summary-is-the-first-view
  // spec:web-ui/agent-session-summary-tab/the-summary-describes-the-session
  test("describes what the session was asked, ran, touched and talked to", async ({ page }) => {
    await signInAsAdminViaForgedSession(page);
    if (!seeded) throw new Error("no session was seeded");
    await page.goto(`/ui/agent-sessions/${seeded.hostId}/${seeded.sessionId}`);
    const views = page.getByRole("group", { name: "Session view" });
    await expect(views.getByRole("button")).toHaveText(["Summary", "Conversation", "Commands", "Processes", "Activity"], { timeout: 20_000 });
    await expect(views.getByRole("button", { name: "Summary", pressed: true })).toBeVisible();

    const glance = page.getByRole("region", { name: "At a glance" });
    await expect(glance.getByText("2 prompts")).toBeVisible({ timeout: 20_000 });
    await expect(glance.getByText("2 commands")).toBeVisible();

    const programs = page.getByRole("list", { name: "Programs by processes run" }).getByRole("listitem");
    await expect(programs).toHaveText(["git1", "npm1"]);

    const touched = page.getByRole("region", { name: "What it touched" });
    await expect(touched.getByText("3 file events across 3 paths: 2 created, 1 modified.")).toBeVisible();
    await expect(touched.getByText("1 path outside this project.")).toBeVisible();
    await expect(touched.getByRole("list", { name: "File events by directory" }).getByRole("listitem"))
      .toHaveText(["./src1 created, 1 modified", "/Users/qa/.npm/_cacache1 created"]);

    const talked = page.getByRole("region", { name: "Where it talked to" });
    // The host's own .local name is a lookup the share test seeds; the Summary, unlike a shared image, shows it.
    await expect(talked.getByText("3 DNS lookups, 1 connection, 2 domains this host had not looked up before.")).toBeVisible();
    await expect(talked.getByRole("list", { name: "Git hosts" })).toContainText("github.com1 lookup, 1 connection");
    await expect(talked.getByRole("list", { name: "Git hosts" }).getByText("New to this host")).toHaveCount(0);
    await expect(talked.getByRole("list", { name: "Package registries" }).getByText("New to this host")).toBeVisible();

    await expect(page.getByRole("region", { name: "Timeline" }).getByRole("img", { name: /^Commands started: 2 across the session/ }))
      .toBeVisible();
    await expect(page.getByRole("region", { name: "Alerts" }).getByText("No alerts were raised in this session.")).toBeVisible();
  });

  // spec:web-ui/a-session-can-be-shared-as-an-image-made-in-the-browser/the-image-is-drawn-in-the-browser-without-a-request
  // spec:web-ui/a-shared-session-image-carries-no-identifying-detail/the-card-shows-only-what-the-person-chose
  test("shares the session as a redacted image made in the browser", async ({ page }) => {
    await signInAsAdminViaForgedSession(page);
    if (!seeded) throw new Error("no session was seeded");
    await page.goto(`/ui/agent-sessions/${seeded.hostId}/${seeded.sessionId}`);
    await page.getByRole("button", { name: "Share as image" }).click({ timeout: 20_000 });

    const dialog = page.getByRole("dialog", { name: "Share this session as an image" });
    const preview = dialog.getByRole("img", { name: /^A summary card of a claude-code/ });
    await expect(preview).toBeVisible({ timeout: 20_000 });
    await expect(dialog.getByText("Nothing is uploaded, and there are no share links.", { exact: false })).toBeVisible();
    await expect(dialog).toMatchAriaSnapshot(`
      - heading "Share this session as an image" [level=2]
      - group "Size":
        - button /Portrait 1080 x 1350/ [pressed]
        - button /Landscape 1200 x 675/
      - group "Hidden unless you show them":
        - switch "Show a prompt as the title"
        - switch "Show the project name"
        - switch "Show internal domains"
        - switch "Show what the credential guard stopped"
    `);

    // The image names neither the user (qa, from the project's home folder) nor the host, and shows its public destinations.
    const svg = decodeURIComponent((await preview.getAttribute("src")) ?? "");
    // The text between the SVG's tags: every other piece when split at angle brackets.
    const words = svg.split(/[<>]/).filter((_, i) => i % 2 === 0).join(" ");
    expect(words).not.toMatch(/\bqa\b|qa-mbp/i);
    expect(words).toContain("Git hosts");
    expect(await preview.getAttribute("alt")).not.toMatch(/\bqa\b|qa-mbp/i);

    // Exporting makes no request: the PNG is drawn in the page.
    const requests: string[] = [];
    page.on("request", (r) => { if (/^https?:/.test(r.url())) requests.push(r.url()); });
    const [download] = await Promise.all([page.waitForEvent("download"), dialog.getByRole("button", { name: "Download PNG" }).click()]);
    expect(download.suggestedFilename()).toBe("rawtrail-session.png");
    const png = await readFile(await download.path());
    expect(png.subarray(1, 4).toString()).toBe("PNG");
    // The IHDR chunk holds the width and height: the portrait card at twice its size.
    expect([png.readUInt32BE(16), png.readUInt32BE(20)]).toEqual([2160, 2700]);
    expect(requests).toEqual([]);
    await expect(dialog.getByRole("status")).toHaveText("Image downloaded.");
  });
});
