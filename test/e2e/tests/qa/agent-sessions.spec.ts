// The AI agent sessions page (issue #1283) and session view (issue #1284), driven from a real ingest: the fake agent posts a Claude
// Code session's four Bash tool calls with the session markers the production agent forwards, the server records the session, and the
// operator finds it on the Agent sessions page, narrows the list to it, and opens it to see what each command ran.
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
  // spec:web-ui/agent-file-activity-views/the-files-tab-lists-an-agent-s-edits
  // spec:web-ui/tool-calls-beside-their-processes/each-bash-group-shows-its-tool-call
  // spec:web-ui/activity-table-cells-stay-on-one-line/a-file-path-in-the-process-detail-panel-reads-whole
  // spec:web-ui/session-processes-view/every-process-is-listed-under-the-agent-that-ran-it
  // spec:web-ui/session-processes-view/a-reparented-process-stays-under-its-creator
  test("a Claude Code session is listed with its project and opens to its commands", async ({ page, agent }) => {
    await signInAsAdminViaForgedSession(page);
    const hostId = crypto.randomUUID();
    await agent.runScenario("claude-code-session.yaml", { hostIdOverride: hostId });
    await waitForSession(hostId);

    await page.goto("/ui/alerts");
    await page.getByRole("link", { name: "Agent sessions", exact: true }).click();
    await expect(page).toHaveURL(/\/ui\/agent-sessions$/);

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
    await expect(page).toHaveURL(new RegExp(`/ui/agent-sessions/${hostId}/${SESSION_ID}$`));
    await expect(page.getByRole("heading", { name: "claude-code 2.1.296" })).toBeVisible({ timeout: 10_000 });

    // One group per command the agent ran, in order, labelled with that command rather than its wrapper script.
    const commands = page.locator(".session-view__call");
    // Four commands: TextEdit is listed under the open that asked for it, not as a command of its own.
    await expect(commands).toHaveCount(4);
    await expect(commands.nth(0).locator("code").first()).toHaveText("git status");
    await expect(commands.nth(1).locator("code").first()).toHaveText("(sleep 47 &)");
    await expect(commands.nth(2).locator("code").first()).toHaveText("sudo id");
    await expect(commands.nth(3).locator("code").first()).toHaveText("open -a TextEdit");
    // Each command names the Bash tool call the agent reported for it in its transcript, its description and tool_use_id, marked as
    // the agent's report rather than what the sensor saw.
    await expect(commands.nth(0).locator(".session-view__agent-call")).toContainText("Show working tree status");
    await expect(commands.nth(0).locator(".session-view__tool-use-id")).toHaveText("toolu_e2e_git");
    await expect(commands.nth(0).locator(".session-view__reported")).toHaveText("Reported by agent");
    await expect(commands.nth(3).locator(".session-view__tool-use-id")).toHaveText("toolu_e2e_open");
    const launch = commands.nth(3).locator("li", { hasText: "inferred launch" }).last();
    await expect(launch.locator(".session-view__name")).toHaveText("TextEdit");
    // Under the open that asked for it, not beside it: the launch's row list belongs to open's row.
    await expect(launch.locator("xpath=../../div/button[contains(@class, 'session-view__name')]")).toHaveText("open");
    // sudo runs as root under the agent's uid-501 shell; id runs as sudo did, so only sudo is badged.
    await expect(commands.nth(2).getByText("runs as root")).toHaveCount(1);
    await expect(commands.nth(2).locator("li", { hasText: "runs as root" }).first().locator(".session-view__name").first()).toHaveText("sudo");
    // The backgrounded sleep is inside its call; the wrapper and the subshell's copy of zsh are plumbing, hidden by default.
    await expect(commands.nth(1).locator(".session-view__name")).toHaveText(["sleep"]);
    await page.getByText("Show shell plumbing").click();
    await expect(commands.nth(1).locator(".session-view__name")).toHaveText(["zsh", "zsh", "sleep"]);

    // Every process as one tree under the agent: the agent leads, marked, and the sleep launchd adopted stays under the subshell that
    // created it, marked as reparented. Nothing is hidden or folded, so the count is the session's eleven and the agent.
    await page.getByRole("group", { name: "Session view" }).getByRole("button", { name: "Processes" }).click();
    const processes = page.locator(".session-view__processes");
    await expect(processes.getByText("12 processes, under the agent that ran them")).toBeVisible();
    await expect(processes.locator(".session-view__name").first()).toHaveText("claude");
    // One root: all four commands' shells sit under the agent, none beside it.
    await expect(processes.locator(":scope > ul > li")).toHaveCount(1);
    await expect(processes.locator(":scope > ul > li > ul > li")).toHaveCount(4);
    await expect(processes.locator("li").first().getByText("agent", { exact: true })).toBeVisible();
    const adopted = processes.locator("li", { hasText: "reparented" }).last();
    await expect(adopted.locator(".session-view__name").first()).toHaveText("sleep");
    await expect(adopted.locator("xpath=../../div/button[contains(@class, 'session-view__name')]")).toHaveText("zsh");
    await page.getByRole("group", { name: "Session view" }).getByRole("button", { name: "Commands" }).click();

    // Selecting a process opens its detail beside the session.
    await commands.nth(1).getByRole("button", { name: "sleep", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Process detail" })).toBeVisible();
    const graphLink = page.getByRole("link", { name: "Open in host graph" });
    await expect(graphLink).toHaveAttribute("href", new RegExp(`/hosts/${hostId}\\?process=\\d+&at=\\d+$`));

    // The session's activity: git's lookup of github.com, its one connection and its index update, matched to git by its generation.
    await page.getByRole("group", { name: "Session view" }).getByRole("button", { name: "Activity" }).click();
    const sessionActivity = page.locator(".session-view__main");
    await expect(sessionActivity.getByText("1 connection to 1 destination")).toBeVisible({ timeout: 10_000 });
    const connection = sessionActivity.locator(".activity-tabs__panel").getByRole("row").nth(1);
    await expect(connection).toContainText("140.82.112.3:443 tcp");
    await expect(connection).toContainText("git (5002)");
    await page.getByRole("group", { name: "Session activity" }).getByRole("button", { name: /^DNS/ }).click();
    await expect(sessionActivity.getByText("1 query for 1 name")).toBeVisible();
    // git's index update, as the agent-file client folded it: one modify standing for two writes, under git's generation.
    await page.getByRole("group", { name: "Session activity" }).getByRole("button", { name: /^Files/ }).click();
    await expect(sessionActivity.getByText("2 file events on 1 path")).toBeVisible();
    const edit = sessionActivity.locator(".activity-tabs__panel").getByRole("row").nth(1);
    await expect(edit).toContainText("modify");
    await expect(edit).toContainText("/Users/dev/project/.git/index");
    await expect(edit).toContainText("git (5002)");

    // The same edit in git's process detail panel, at a width that puts the panel below the commands: the path keeps to one line and
    // the table scrolls sideways, rather than wrapping the path a few characters at a time.
    await page.setViewportSize({ width: 800, height: 900 });
    await page.getByRole("group", { name: "Session view" }).getByRole("button", { name: "Commands" }).click();
    await commands.nth(0).getByRole("button", { name: "git", exact: true }).click();
    await page.getByRole("group", { name: "Process activity" }).getByRole("button", { name: /^Files/ }).click();
    const panelRow = page.locator(".process-detail__activity .activity-tabs__panel").getByRole("row").nth(1);
    await expect(panelRow).toContainText("/Users/dev/project/.git/index");
    // The lines the path's text sits on, by the distinct tops of its rendered boxes: cells share their row's height, so a cell's own
    // box cannot tell one line from several.
    const pathLines = await panelRow.locator("td").nth(1).evaluate((cell) => {
      const range = document.createRange();
      range.selectNodeContents(cell);
      return new Set(Array.from(range.getClientRects(), (r) => Math.round(r.top))).size;
    });
    expect(pathLines).toBe(1);
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
