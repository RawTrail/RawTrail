// The AI agent sessions page (issue #1283), driven from a real ingest: the fake agent posts a Claude Code session's execs with the
// session markers the production agent forwards, the server records the session, and the operator finds it on the Sessions page,
// narrows the list to it, and opens it.
//
// The host id is fresh per run and the list is filtered to it, so rows other specs or an operator left on the dev database cannot
// stand in for this session.

import * as crypto from "node:crypto";
import { test, expect } from "../../fixtures/agent";
import { signInAsAdminViaForgedSession } from "../../fixtures/auth";
import { openDB } from "../../fixtures/db";

const SESSION_ID = "169b5bb1-29b5-41da-9a54-92cd72599ad9";

test.describe("agent sessions", () => {
  // Above the default 30s: enrol, post, ingest and build the graph before the page has anything to show.
  test.setTimeout(90_000);

  // spec:web-ui/ai-agent-sessions-page/sessions-are-listed-with-their-project-and-activity
  // spec:web-ui/ai-agent-sessions-page/a-session-opens-its-activity
  test("a Claude Code session is listed with its project and opens its host", async ({ page, agent }) => {
    await signInAsAdminViaForgedSession(page);
    const hostId = crypto.randomUUID();
    await agent.runScenario("claude-code-session.yaml", { hostIdOverride: hostId });
    await waitForSession(hostId);

    await page.goto("/ui/alerts");
    await page.getByRole("link", { name: "Sessions", exact: true }).click();
    await expect(page).toHaveURL(/\/ui\/sessions$/);

    // Narrow to this run's host through the page's own filter, which the server applies.
    await page.getByLabel("Add filter field").selectOption("host_id");
    await page.getByLabel("Host value").fill(hostId);
    await page.getByRole("button", { name: "Add" }).click();
    await expect(page).toHaveURL(new RegExp(`host_id=${hostId}`));

    const rows = page.locator("tbody tr");
    await expect(rows).toHaveCount(1, { timeout: 10_000 });
    const cells = rows.first().locator("td");
    await expect(cells.nth(0)).toHaveText("claude-code 2.1.296");
    await expect(cells.nth(1)).toHaveText("/Users/dev/project");
    // Both tool-call processes carry the session: the shell and the git it ran.
    await expect(cells.nth(5)).toHaveText("2");
    await expect(cells.nth(7)).toHaveText(SESSION_ID.slice(0, 8));

    await rows.first().getByRole("link", { name: "claude-code" }).click();
    await expect(page).toHaveURL(new RegExp(`/ui/hosts/${hostId}\\?at=\\d+$`));
    const details = page.getByRole("button", { name: "Details" });
    await expect(details).toBeVisible({ timeout: 15_000 });
    await details.click();
    await expect(page.getByText(hostId)).toBeVisible();
  });
});

// waitForSession polls until the graph builder has recorded the session and stamped both of its processes. The page is not opened
// before that: an empty list would be the ingest's latency, not the page's behavior.
async function waitForSession(hostId: string): Promise<void> {
  const db = await openDB();
  try {
    await expect
      .poll(
        async () => {
          const [rows] = (await db.query(
            `SELECT
               (SELECT COUNT(*) FROM agent_sessions WHERE host_id = ? AND agent_session_id = ?) AS sessions,
               (SELECT COUNT(*) FROM processes WHERE host_id = ? AND agent_session_id = ?) AS processes`,
            [hostId, SESSION_ID, hostId, SESSION_ID],
          )) as [Array<{ sessions: number | string; processes: number | string }>, unknown];
          return `${String(rows[0].sessions)}/${String(rows[0].processes)}`;
        },
        { timeout: 30_000, message: "the session and its two processes were never recorded" },
      )
      .toBe("1/2");
  } finally {
    await db.end();
  }
}
