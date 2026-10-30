// The sessions list's bypass filter (issue #1427), driven from a real ingest: the fake agent posts two Claude Code sessions, one of
// whose assistant messages were written with permissions bypassed, the graph builder records that one, and the operator narrows the
// list to it.
//
// The host id is fresh per run and the list is filtered to it, so rows other specs left on the dev database cannot stand in.

import * as crypto from "node:crypto";
import { test, expect } from "../../fixtures/agent";
import { signInAsAdminViaForgedSession } from "../../fixtures/auth";
import { openDB } from "../../fixtures/db";

const BYPASSED = "5b2f6e3a-1c4d-4e8f-9a0b-7c6d5e4f3a21";

test.describe("agent sessions bypass filter", () => {
  // Above the default 30s: enrol, post, ingest and build the graph before the page has anything to show.
  test.setTimeout(90_000);

  // spec:web-ui/sessions-list-bypass-filter/the-list-narrows-to-sessions-that-ran-bypassed
  // spec:web-ui/sessions-list-tokens/a-session-lists-its-tokens
  test("narrows the list to the session that ran bypassed", async ({ page, agent }) => {
    await signInAsAdminViaForgedSession(page);
    const hostId = crypto.randomUUID();
    await agent.runScenario("claude-code-bypass-sessions.yaml", { hostIdOverride: hostId });
    await waitForBypass(hostId);

    await page.goto(`/ui/agent-sessions?host_id=${hostId}`);
    const rows = page.locator("tbody tr");
    await expect(rows).toHaveCount(2, { timeout: 10_000 });
    const badge = page.getByText("Bypass permissions");
    await expect(badge).toHaveCount(1);
    await expect(rows.filter({ hasText: "/Users/dev/bypassed" })).toContainText("Bypass permissions");
    await expect(rows.filter({ hasText: "/Users/dev/careful" })).not.toContainText("Bypass permissions");
    // Each scenario message used 10 input and 20 output tokens; the bypassed session sent two, the other one.
    const column = (await page.getByRole("columnheader").allTextContents()).indexOf("Tokens");
    expect(column).toBeGreaterThan(-1);
    await expect(rows.filter({ hasText: "/Users/dev/bypassed" }).locator("td").nth(column)).toHaveText("60");
    await expect(rows.filter({ hasText: "/Users/dev/careful" }).locator("td").nth(column)).toHaveText("30");

    await page.getByLabel("Add filter field").selectOption("bypass");
    await page.getByLabel("Permissions value").selectOption("true");
    await page.getByRole("button", { name: "Add" }).click();
    await expect(page).toHaveURL(/bypass=true/);
    await expect(page.getByText("Permissions: Ran in bypass mode (or forked from a session that did)")).toBeVisible();
    await expect(rows).toHaveCount(1);
    await expect(rows.first()).toContainText("/Users/dev/bypassed");
    await expect(page.getByText("Showing 1 of 1 matches")).toBeVisible();
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
