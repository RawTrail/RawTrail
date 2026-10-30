// The Conversation view (issue #1322): a session read as its turns, each prompt the person typed with the commands and tool calls the
// agent made for it. The session is written straight into the dev databases, as the agent and the server would have left it, on a
// fresh host. The test removes its MySQL rows afterwards; its archive events stay on a host nothing else names until the archive's
// TTL expires them, since a ClickHouse delete rewrites every part of the events table.

import * as crypto from "node:crypto";
import { test, expect } from "@playwright/test";
import { signInAsAdminViaForgedSession } from "../../fixtures/auth";
import { insertClickHouse, openDB } from "../../fixtures/db";

interface Seeded {
  hostId: string;
  sessionId: string;
  // token is this run's own word in the second prompt, so a search across every session finds this run's prompt alone.
  token: string;
}

// The two commands the agent ran, one per prompt: the label the server reads from each wrapper is the Bash call's command.
const STEPS = ["git status", "npm test"];

async function seedSession(): Promise<Seeded> {
  const hostId = crypto.randomUUID();
  const sessionId = crypto.randomUUID();
  const token = `qa${crypto.randomUUID().slice(0, 8)}`;
  const base = 910_000_000_000 + crypto.randomInt(1_000_000) * 1_000;
  const startNs = (Date.now() - 3_600_000) * 1_000_000;
  const rows: unknown[][] = [];
  STEPS.forEach((step, i) => {
    const shellId = base + i * 10;
    const shellPid = 41_000 + i * 10;
    const at = startNs + (i + 1) * 60_000_000_000;
    const script = `source /tmp/snap.sh && eval '${step}' < /dev/null`;
    rows.push([shellId, shellPid, 1, "/bin/zsh", JSON.stringify(["/bin/zsh", "-c", script]), at, shellPid, null, i * 10 + 1]);
    const [tool, ...args] = step.split(" ");
    rows.push([shellId + 1, shellPid + 1, shellPid, `/usr/bin/${tool}`, JSON.stringify([tool, ...args]), at + 1_000, shellPid, shellId,
      i * 10 + 2]);
  });
  const db = await openDB();
  try {
    await db.query(
      `INSERT INTO processes (id, host_id, pid, ppid, path, args, fork_time_ns, tool_call_group, parent_process_id, pidversion, uid,
         exec_time_ns, exit_time_ns, exit_reason, is_snapshot, agent_session_id, agent_name, agent_version) VALUES ` +
        rows.map(() => "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 501, ?, ?, 'event', 0, ?, 'claude-code', '2.1.311')").join(", "),
      rows.flatMap((r) => [r[0], hostId, ...r.slice(1), Number(r[5]) + 1, Number(r[5]) + 5_000_000, sessionId]),
    );
    await db.query(
      `INSERT INTO agent_sessions (host_id, agent_session_id, agent_name, agent_version, agent_pid, cwd, first_seen_ns, first_event_id,
         last_seen_ns) VALUES (?, ?, 'claude-code', '2.1.311', 39998, '/Users/qa/conversation', ?, ?, ?)`,
      [hostId, sessionId, startNs, `conversation-${hostId}`, startNs + 10 * 60_000_000_000],
    );
  } catch (err) {
    await db.query("DELETE FROM processes WHERE host_id = ?", [hostId]);
    await db.query("DELETE FROM agent_sessions WHERE host_id = ?", [hostId]);
    throw err;
  } finally {
    await db.end();
  }
  const event = (id: string, type: string, atNs: number, payload: Record<string, unknown>) => ({
    event_id: `${id}-${hostId}`, host_id: hostId, timestamp_ns: atNs, ingested_at_ns: atNs + 1, event_type: type, platform: "darwin",
    payload: JSON.stringify({ agent_session_id: sessionId, uid: 501, ...payload }),
  });
  const turnAt = (i: number) => startNs + (i + 1) * 60_000_000_000;
  await insertClickHouse("events", [
    event("prompt-1", "agent_prompt", turnAt(0) - 5_000_000_000, { prompt_id: "turn-1", text: "check the repo state", length: 20 }),
    event("prompt-2", "agent_prompt", turnAt(1) - 5_000_000_000, { prompt_id: "turn-2", text: `now run the tests ${token}`,
      length: 28 }),
    event("call-git", "agent_tool_call", turnAt(0) - 1_000_000_000, { tool_use_id: "toolu_e2e_git", tool_name: "Bash", command: STEPS[0],
      prompt_id: "turn-1" }),
    event("call-read", "agent_tool_call", turnAt(0) - 500_000_000, { tool_use_id: "toolu_e2e_read", tool_name: "Read",
      file_path: "/Users/qa/conversation/package.json", prompt_id: "turn-1" }),
    event("call-test", "agent_tool_call", turnAt(1) - 1_000_000_000, { tool_use_id: "toolu_e2e_test", tool_name: "Bash", command: STEPS[1],
      prompt_id: "turn-2" }),
  ]).catch(async (err: unknown) => {
    // The MySQL rows are written: remove them, since afterEach never sees a session the seed did not return.
    await removeSession({ hostId, sessionId, token });
    throw err;
  });
  return { hostId, sessionId, token };
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

test.describe("the conversation view", () => {
  test.setTimeout(90_000);
  let seeded: Seeded | undefined;
  test.beforeEach(async () => { seeded = await seedSession(); });
  test.afterEach(async () => {
    if (seeded) await removeSession(seeded);
    seeded = undefined;
  });

  // spec:web-ui/agent-session-conversation-view/each-prompt-shows-what-the-agent-did-for-it
  test("lists each prompt with the commands and tool calls the agent made for it", async ({ page }) => {
    await signInAsAdminViaForgedSession(page);
    if (!seeded) throw new Error("no session was seeded");
    await page.goto(`/ui/agent-sessions/${seeded.hostId}/${seeded.sessionId}`);
    await page.getByRole("button", { name: "Conversation", exact: true }).click();

    const turns = page.locator(".session-view__turn");
    await expect(turns).toHaveCount(2, { timeout: 20_000 });
    const first = turns.nth(0);
    await expect(first.locator(".session-view__prompt-text")).toHaveText("check the repo state");
    await expect(first.locator(".session-view__prompt .session-view__reported")).toHaveText("Reported by agent");
    await expect(first.locator(".session-view__command").first()).toHaveText("git status");
    // The command is paired with the Bash call the agent made for it in this turn.
    await expect(first.locator(".session-view__tool-use-id")).toHaveText("toolu_e2e_git");
    await expect(first.locator(".session-view__turn-call")).toContainText("/Users/qa/conversation/package.json");
    const second = turns.nth(1);
    await expect(second.locator(".session-view__prompt-text")).toHaveText(`now run the tests ${seeded.token}`);
    await expect(second.locator(".session-view__command").first()).toHaveText("npm test");

    await page.getByRole("searchbox", { name: "Search this session" }).fill("run the TESTS");
    await expect(turns).toHaveCount(1);
    await expect(turns.first().locator(".session-view__prompt-text")).toHaveText(`now run the tests ${seeded.token}`);
  });

  // spec:web-ui/agent-prompt-search/an-operator-finds-a-prompt-across-sessions
  test("finds the prompt across every session and opens it in its conversation", async ({ page }) => {
    await signInAsAdminViaForgedSession(page);
    if (!seeded) throw new Error("no session was seeded");
    await page.goto("/ui/agent-sessions");
    const search = page.getByRole("region", { name: "Search prompts" });
    await search.getByRole("searchbox", { name: "Prompt text" }).fill(seeded.token.toUpperCase());
    await search.getByRole("button", { name: "Search" }).click();
    const rows = search.locator("tbody tr");
    await expect(rows).toHaveCount(1, { timeout: 20_000 });
    await expect(rows.first().locator("mark")).toHaveText(seeded.token);
    await rows.first().getByRole("link").click();
    await expect(page).toHaveURL(new RegExp(`/ui/agent-sessions/${seeded.hostId}/${seeded.sessionId}\\?view=conversation$`));
    await expect(page.getByRole("button", { name: "Conversation", exact: true })).toHaveAttribute("aria-pressed", "true");
    await expect(page.locator(".session-view__prompt-text").nth(1)).toHaveText(`now run the tests ${seeded.token}`);
  });
});
