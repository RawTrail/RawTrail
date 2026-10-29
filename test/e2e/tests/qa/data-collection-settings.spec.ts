// Data collection settings (issue #1426): the page says whether agents capture the prompts people type to AI coding agents, and an
// administrator turns capture off with a reason, which the server stores, audits and pushes. The setting is fleet-wide, so the test
// puts back the value it found, through the same API, once it is done.

import { test, expect, type APIRequestContext } from "@playwright/test";
import { signInAsAdminViaForgedSession } from "../../fixtures/auth";
import { openDB } from "../../fixtures/db";

const ROUTE = "/api/v1/detection-config/agent-settings";

interface Settings {
  version: number;
  capture_prompts: boolean;
}

async function readSettings(request: APIRequestContext): Promise<Settings> {
  const resp = await request.get(ROUTE);
  expect(resp.status()).toBe(200);
  return (await resp.json()) as Settings;
}

test.describe("Data collection settings", () => {
  let csrf = "";
  let found: Settings | undefined;

  test.afterEach(async ({ page }) => {
    if (found === undefined) return;
    const now = await readSettings(page.request);
    if (now.capture_prompts !== found.capture_prompts) {
      const resp = await page.request.put(ROUTE, {
        headers: { "X-CSRF-Token": csrf },
        data: { capture_prompts: found.capture_prompts, reason: "e2e: restore the setting found", expected_version: now.version },
      });
      expect(resp.status()).toBe(200);
    }
    found = undefined;
  });

  // spec:web-ui/data-collection-settings-show-and-change-prompt-capture/the-page-says-plainly-that-prompts-are-captured
  // spec:web-ui/data-collection-settings-show-and-change-prompt-capture/turning-capture-off-asks-for-a-reason-and-reports-the-push
  test("says whether prompts are captured, and turns capture off with a reason", async ({ page }) => {
    await signInAsAdminViaForgedSession(page);
    const session = await page.request.get("/api/session");
    csrf = ((await session.json()) as { csrf_token: string }).csrf_token;
    found = await readSettings(page.request);
    if (!found.capture_prompts) {
      // Start from capture on, which is what a fresh deployment has.
      const on = await page.request.put(ROUTE, {
        headers: { "X-CSRF-Token": csrf },
        data: { capture_prompts: true, reason: "e2e: start from capture on", expected_version: found.version },
      });
      expect(on.status()).toBe(200);
    }

    await page.goto("/ui/admin/settings/data-collection");
    await expect(page.getByRole("link", { name: "Data collection" })).toBeVisible();
    const toggle = page.getByRole("switch", { name: "Capture prompts" });
    await expect(toggle).toBeChecked({ timeout: 20_000 });
    await expect(page.getByText(/^Prompt capture is on\. Agents send the prompts people type/)).toBeVisible();
    await expect(page.getByText("EDR_CAPTURE_PROMPTS=0", { exact: true })).toBeVisible();

    // The switch is a visually hidden checkbox inside its label, so the label is what a person clicks.
    await page.getByText("Capture prompts", { exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "Turn prompt capture off?" });
    await expect(dialog).toBeVisible();
    await dialog.getByRole("textbox").fill("e2e: works council review");
    await dialog.getByRole("button", { name: "Turn off" }).click();

    await expect(page.getByRole("status").filter({ hasText: /^Saved as version \d+\. Queued for \d+ of \d+ enrolled/ })).toBeVisible();
    await expect(toggle).not.toBeChecked();
    await expect(page.getByText(/^Prompt capture is off\./)).toBeVisible();
    const stored = await readSettings(page.request);
    expect(stored.capture_prompts).toBe(false);

    // The audit row is delivered by the outbox drain shortly after the change commits.
    const db = await openDB();
    try {
      await expect.poll(async () => {
        const [rows] = (await db.query(
          `SELECT JSON_UNQUOTE(JSON_EXTRACT(payload, '$.reason')) AS reason FROM audit_events
            WHERE action = 'detection_config.agent_settings_update' AND target_id = ?`,
          [String(stored.version)],
        )) as [Array<{ reason: string }>, unknown];
        return rows.map((r) => r.reason);
      }, { timeout: 30_000 }).toEqual(["e2e: works council review"]);
    } finally {
      await db.end();
    }
  });
});
