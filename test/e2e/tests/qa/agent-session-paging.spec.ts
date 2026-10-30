// A session larger than one page of everything the session view reads (issues #1425 and #1452): its commands, its process tree and
// its DNS names each run past a page, and the operator pages through them and searches each. A session this size is past what the
// fake agent can post in a test's time, so it is written straight into the dev databases, as the server's own reads would find it,
// on a fresh host. The test removes its MySQL rows afterwards. Its DNS events stay in the archive, on a host nothing else names, until
// the archive's TTL expires them: a ClickHouse delete is a mutation that rewrites every part of the events table, which on a dev
// archive holding a VM's telemetry costs minutes of CPU for a few hundred rows.

import * as crypto from "node:crypto";
import { test, expect } from "@playwright/test";
import { signInAsAdminViaForgedSession } from "../../fixtures/auth";
import { insertClickHouse, openDB } from "../../fixtures/db";

// 60 commands of 86 processes each: past a 50-command page and past the tree's 5,000-process page.
const COMMANDS = 60;
const CHILDREN = 85;
// 250 DNS names: past the activity rollup's 200-row list.
const DNS_NAMES = 250;

interface Seeded {
  hostId: string;
  sessionId: string;
}

// seedSession writes the session's processes and record into MySQL and its DNS lookups into the ClickHouse archive.
async function seedSession(): Promise<Seeded> {
  const hostId = crypto.randomUUID();
  const sessionId = crypto.randomUUID();
  // Explicit row ids well above any the dev database hands out, so a creator's id is known when its children are written.
  const base = 900_000_000_000 + crypto.randomInt(1_000_000) * 1_000;
  const startNs = (Date.now() - 3_600_000) * 1_000_000;
  const rows: unknown[][] = [];
  let pid = 40_000;
  for (let c = 0; c < COMMANDS; c++) {
    const shellId = base + c * 100;
    const shellPid = pid++;
    const at = startNs + c * 1_000_000_000;
    const script = `source /tmp/snap.sh && eval 'step ${String(c)} && echo done' < /dev/null`;
    rows.push([shellId, hostId, shellPid, 1, "/bin/zsh", JSON.stringify(["/bin/zsh", "-c", script]), at, shellPid, null, c * 100 + 1]);
    for (let k = 0; k < CHILDREN; k++) {
      rows.push([shellId + 1 + k, hostId, pid++, shellPid, "/bin/echo", JSON.stringify(["echo", `marker-${String(c)}-${String(k)}`]),
        at + (k + 1) * 1_000, shellPid, shellId, c * 100 + 2 + k]);
    }
  }
  const db = await openDB();
  // A seed that fails part way removes what it wrote, since afterEach never sees a session the seed did not return.
  try {
    for (let i = 0; i < rows.length; i += 1_000) {
      const chunk = rows.slice(i, i + 1_000);
      await db.query(
        `INSERT INTO processes (id, host_id, pid, ppid, path, args, fork_time_ns, tool_call_group, parent_process_id, pidversion, uid,
           exec_time_ns, exit_time_ns, exit_reason, is_snapshot, agent_session_id, agent_name, agent_version) VALUES ` +
          chunk.map(() => "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 501, ?, ?, 'event', 0, ?, 'claude-code', '2.1.302')").join(", "),
        chunk.flatMap((r) => [...r, Number(r[6]) + 1, Number(r[6]) + 5_000_000, sessionId]),
      );
    }
    await db.query(
      `INSERT INTO agent_sessions (host_id, agent_session_id, agent_name, agent_version, agent_pid, cwd, first_seen_ns, first_event_id,
         last_seen_ns) VALUES (?, ?, 'claude-code', '2.1.302', 39999, '/Users/qa/paging', ?, ?, ?)`,
      [hostId, sessionId, startNs, `paging-${hostId}`, startNs + (COMMANDS + 10) * 1_000_000_000],
    );
  } catch (err) {
    await db.query("DELETE FROM processes WHERE host_id = ?", [hostId]);
    await db.query("DELETE FROM agent_sessions WHERE host_id = ?", [hostId]);
    throw err;
  } finally {
    await db.end();
  }
  // One lookup per child of the first commands, each a distinct name, made by that child's process generation.
  await insertClickHouse("events", Array.from({ length: DNS_NAMES }, (_, i) => {
    const row = rows[Math.floor(i / CHILDREN) * (CHILDREN + 1) + 1 + (i % CHILDREN)];
    const ts = Number(row[6]) + 2_000;
    return {
      event_id: `paging-dns-${hostId}-${String(i)}`, host_id: hostId, timestamp_ns: ts, ingested_at_ns: ts + 1,
      event_type: "dns_query", platform: "darwin",
      payload: JSON.stringify({ pid: row[2], pidversion: row[9], query_name: `n${String(i).padStart(3, "0")}.paging.example`,
        query_type: "A" }),
    };
  }));
  return { hostId, sessionId };
}

async function removeSession({ hostId }: Seeded): Promise<void> {
  const db = await openDB();
  try {
    await db.query("DELETE FROM processes WHERE host_id = ?", [hostId]);
    await db.query("DELETE FROM agent_sessions WHERE host_id = ?", [hostId]);
  } finally {
    await db.end();
  }
}

test.describe("a session larger than one page", () => {
  // Seeding five thousand processes and reading a session this size takes longer than the default 30s.
  test.setTimeout(120_000);
  let seeded: Seeded;
  test.beforeEach(async () => { seeded = await seedSession(); });
  test.afterEach(async () => { await removeSession(seeded); });

  // spec:web-ui/agent-session-view-pages-its-commands/the-rest-of-the-commands-come-on-request
  // spec:web-ui/agent-session-view-pages-its-processes-and-activity/the-rest-of-the-processes-come-on-request
  // spec:web-ui/agent-session-view-pages-its-processes-and-activity/the-rest-of-an-activity-list-comes-on-request
  // spec:web-ui/the-session-view-searches-its-processes-and-activity-on-the-server/a-processes-search-finds-a-process-not-yet-loaded
  // spec:web-ui/the-session-view-searches-its-processes-and-activity-on-the-server/an-activity-search-reads-the-class-from-the-server
  test("pages and searches its commands, processes and activity", async ({ page }) => {
    await signInAsAdminViaForgedSession(page);
    await page.goto(`/ui/agent-sessions/${seeded.hostId}/${seeded.sessionId}`);
    await expect(page.locator("header").getByText(`${String(COMMANDS)} commands`)).toBeVisible({ timeout: 20_000 });
    const search = page.getByRole("searchbox", { name: "Search this session" });
    await page.getByRole("button", { name: "Commands", exact: true }).click();

    // Commands: a page of 50, the rest on request, and a search the server answers.
    const commands = page.locator(".session-view__call");
    await expect(commands).toHaveCount(50);
    await page.getByRole("button", { name: "Load more commands (10 more)" }).click();
    await expect(commands).toHaveCount(COMMANDS);
    await search.fill("step 59 ");
    await expect(commands).toHaveCount(1);
    await expect(commands.first().locator("code").first()).toHaveText("step 59 && echo done");
    await search.fill("");

    // Processes: the first tree page holds 5,000 of the session's processes, and the last command's children lie past it. A search
    // finds one of them all the same, from the server.
    await page.getByRole("button", { name: "Processes", exact: true }).click();
    await expect(page.getByRole("button", { name: "Load more processes" })).toBeVisible({ timeout: 20_000 });
    await search.fill(`marker-${String(COMMANDS - 1)}-${String(CHILDREN - 1)}`);
    await expect(page.getByText("1 process matches")).toBeVisible();
    await expect(page.getByRole("button", { name: "echo" })).toHaveCount(1);
    await search.fill("");

    // Activity: the DNS list stops at 200 of the 250 names; the rest come on request, and a search finds a name past the list.
    await page.getByRole("button", { name: "Activity", exact: true }).click();
    await page.getByRole("button", { name: /^DNS/ }).click();
    await expect(page.getByText("n000.paging.example")).toBeVisible({ timeout: 20_000 });
    await expect(page.getByText("n249.paging.example")).toHaveCount(0);
    await page.getByRole("button", { name: "Load more rows" }).click();
    await expect(page.getByText("n249.paging.example")).toBeVisible();
    await search.fill("n240.paging");
    await expect(page.getByText("n240.paging.example")).toBeVisible();
    await expect(page.getByText("n000.paging.example")).toHaveCount(0);
  });
});
