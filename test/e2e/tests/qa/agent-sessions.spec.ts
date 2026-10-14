// The AI agent sessions page (issue #1283) and session view (issue #1284), driven from a real ingest: the fake agent posts a Claude
// Code session's four Bash tool calls with the session markers the production agent forwards, the server records the session, and the
// operator finds it on the Sessions page, narrows the list to it, and opens it to see what each tool call ran.
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
  // spec:web-ui/ai-agent-session-view/tool-calls-are-labelled-with-the-command-the-agent-ran
  // spec:web-ui/ai-agent-session-view/a-daemon-the-agent-started-stays-in-its-tool-call
  // spec:web-ui/ai-agent-session-view-badges/a-privilege-change-is-badged
  // spec:web-ui/ai-agent-session-view-navigation/a-process-opens-its-detail-beside-the-session
  // spec:web-ui/activity-tabs/a-session-s-network-tab-lists-every-destination-with-its-process
  // spec:web-ui/inferred-launches-in-the-ui/a-session-lists-a-launch-under-the-call-that-asked
  test("a Claude Code session is listed with its project and opens to its tool calls", async ({ page, agent }) => {
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
    // Every process carries the session: four wrapper shells, the git, the subshell's copy of zsh, the sleep it backgrounded, sudo
    // and the id it ran, the open, and TextEdit, which launchd started with the agent's environment.
    await expect(cells.nth(5)).toHaveText("11");
    await expect(cells.nth(7)).toHaveText(SESSION_ID.slice(0, 8));

    await rows.first().getByRole("link", { name: "claude-code" }).click();
    await expect(page).toHaveURL(new RegExp(`/ui/sessions/${hostId}/${SESSION_ID}$`));
    await expect(page.getByRole("heading", { name: "claude-code 2.1.296" })).toBeVisible({ timeout: 10_000 });

    // One group per tool call, in order, labelled with the command the agent ran rather than its wrapper script.
    const calls = page.locator(".session-view__call");
    // Four calls: TextEdit is listed under the open that asked for it, not as a call of its own.
    await expect(calls).toHaveCount(4);
    await expect(calls.nth(0).locator("code")).toHaveText("git status");
    await expect(calls.nth(1).locator("code")).toHaveText("(sleep 47 &)");
    await expect(calls.nth(2).locator("code")).toHaveText("sudo id");
    await expect(calls.nth(3).locator("code")).toHaveText("open -a TextEdit");
    const launch = calls.nth(3).locator("li", { hasText: "inferred launch" }).last();
    await expect(launch.locator(".session-view__name")).toHaveText("TextEdit");
    // sudo runs as root under the agent's uid-501 shell; id runs as sudo did, so only sudo is badged.
    await expect(calls.nth(2).getByText("runs as root")).toHaveCount(1);
    await expect(calls.nth(2).locator("li", { hasText: "runs as root" }).first().locator(".session-view__name").first()).toHaveText("sudo");
    // The backgrounded sleep is inside its call; the wrapper and the subshell's copy of zsh are plumbing, hidden by default.
    await expect(calls.nth(1).locator(".session-view__name")).toHaveText(["sleep"]);
    await page.getByText("Show shell plumbing").click();
    await expect(calls.nth(1).locator(".session-view__name")).toHaveText(["zsh", "zsh", "sleep"]);

    // Selecting a process opens its detail beside the session.
    await calls.nth(1).getByRole("button", { name: "sleep", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Process detail" })).toBeVisible();
    const graphLink = page.getByRole("link", { name: "Open in host graph" });
    await expect(graphLink).toHaveAttribute("href", new RegExp(`/hosts/${hostId}\\?process=\\d+&at=\\d+$`));

    // The session's activity: git's lookup of github.com and its one connection, matched to git by its generation.
    await page.getByRole("group", { name: "Session view" }).getByRole("button", { name: "Activity" }).click();
    const sessionActivity = page.locator(".session-view__main");
    await expect(sessionActivity.getByText("1 connection to 1 destination")).toBeVisible({ timeout: 10_000 });
    const connection = sessionActivity.locator(".activity-tabs__panel").getByRole("row").nth(1);
    await expect(connection).toContainText("140.82.112.3:443 tcp");
    await expect(connection).toContainText("git (5002)");
    await page.getByRole("group", { name: "Session activity" }).getByRole("button", { name: /^DNS/ }).click();
    await expect(sessionActivity.getByText("1 query for 1 name")).toBeVisible();
  });
});

// waitForSession polls until the graph builder has recorded the session and stamped all eleven of its processes. The page is not
// opened before that: an empty list would be the ingest's latency, not the page's behavior.
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
        { timeout: 30_000, message: "the session and its eleven processes were never recorded" },
      )
      .toBe("1/11");
  } finally {
    await db.end();
  }
}
