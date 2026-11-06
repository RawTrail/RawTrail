// The sessions list's bypass badge (issue #1427), driven from a real ingest: the fake agent posts two Claude Code sessions, one of
// whose assistant messages were written with permissions bypassed, and the graph builder records that one.
//
// The host id is fresh per run and the rows are found by their links to that host, so rows other specs left on the dev database
// cannot stand in. The fake agent stamps the run's events from now, so its sessions are among the newest.

import * as crypto from "node:crypto";
import { test, expect } from "../../fixtures/agent";
import { signInAsAdminViaForgedSession } from "../../fixtures/auth";
import { openDB } from "../../fixtures/db";

const BYPASSED = "5b2f6e3a-1c4d-4e8f-9a0b-7c6d5e4f3a21";

test.describe("agent sessions bypass badge", () => {
  // Above the default 30s: enrol, post, ingest and build the graph before the page has anything to show.
  test.setTimeout(90_000);

  // spec:web-ui/sessions-list-bypass-badge/a-bypassed-session-s-row-is-badged
  // spec:web-ui/sessions-list-tokens/a-session-lists-its-tokens
  test("badges the session that ran bypassed", async ({ page, agent }) => {
    await signInAsAdminViaForgedSession(page);
    const hostId = crypto.randomUUID();
    await agent.runScenario("claude-code-bypass-sessions.yaml", { hostIdOverride: hostId });
    await waitForBypass(hostId);

    await page.goto("/ui/agent-sessions");
    const rows = page.locator(`tbody tr:has(a[href*="/agent-sessions/${hostId}/"])`);
    await expect(rows).toHaveCount(2, { timeout: 10_000 });
    await expect(rows.getByText("Bypass permissions")).toHaveCount(1);
    await expect(rows.filter({ hasText: "/Users/dev/bypassed" })).toContainText("Bypass permissions");
    await expect(rows.filter({ hasText: "/Users/dev/careful" })).not.toContainText("Bypass permissions");
    // Each scenario message used 10 input and 20 output tokens; the bypassed session sent two, the other one.
    const column = (await page.getByRole("columnheader").allTextContents()).indexOf("Tokens");
    expect(column).toBeGreaterThan(-1);
    await expect(rows.filter({ hasText: "/Users/dev/bypassed" }).locator("td").nth(column)).toHaveText("60");
    await expect(rows.filter({ hasText: "/Users/dev/careful" }).locator("td").nth(column)).toHaveText("30");
  });
});

// waitForBypass polls until the graph builder has recorded both sessions and the bypassed one's mode. The page is not opened before
// that: a list without the badge would be the ingest's latency, not the page's behavior.
async function waitForBypass(hostId: string): Promise<void> {
  const db = await openDB();
  try {
    await expect
      .poll(
        async () => {
          const [rows] = (await db.query(
            `SELECT
               (SELECT COUNT(*) FROM agent_sessions WHERE host_id = ?) AS sessions,
               (SELECT COUNT(*) FROM agent_session_bypass WHERE host_id = ? AND agent_session_id = ?) AS bypassed`,
            [hostId, hostId, BYPASSED],
          )) as [Array<{ sessions: number | string; bypassed: number | string }>, unknown];
          return `${String(rows[0].sessions)}/${String(rows[0].bypassed)}`;
        },
        { timeout: 30_000, message: "the two sessions and the bypassed one's mode were never recorded" },
      )
      .toBe("2/1");
  } finally {
    await db.end();
  }
}
