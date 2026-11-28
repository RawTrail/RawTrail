import type { Connection } from "mysql2/promise";
import { test, expect } from "../../fixtures/test";
import { openDB, resetDB, promote } from "../../fixtures/db";
import { dexPassword, signInViaDex } from "./_setup";

// An application control write on a stale session asks for a fresh sign-in (issue #1575), and the Add rule dialog keeps its unsaved
// rule across the identity provider round trip (#1419): nothing is stored until the operator saves again on the fresh session.
//
// Runs against the qa-oidc dev server the Playwright config starts (local dex). The session is aged with SQL rather than by waiting
// out the window, as the reauth modal spec does.

const ADMIN_EMAIL = "admin@qa.local";
const IDENTIFIER = "b".repeat(64);
const REASON = "qa e2e: application control needs a fresh sign-in";

async function defaultPolicyID(db: Connection): Promise<number> {
  const [rows] = (await db.query("SELECT id FROM app_control_policies WHERE name = 'Default' LIMIT 1")) as [
    Array<{ id: number | string }>,
    unknown,
  ];
  if (rows.length === 0) throw new Error("defaultPolicyID: the seed 'Default' policy is missing");
  return Number(rows[0].id);
}

async function storedIdentifiers(policyID: number): Promise<string[]> {
  const db = await openDB();
  try {
    const [rows] = (await db.query("SELECT identifier FROM app_control_rules WHERE policy_id = ?", [policyID])) as [
      Array<{ identifier: string }>,
      unknown,
    ];
    return rows.map((r) => r.identifier);
  } finally {
    await db.end();
  }
}

test.describe.serial("application control asks for a fresh sign-in", () => {
  let policyID: number;

  test.beforeAll(async ({ browser }) => {
    const ctx = await browser.newContext();
    try {
      const page = await ctx.newPage();
      const db = await openDB();
      try {
        await resetDB(db);
        policyID = await defaultPolicyID(db);
        await db.query("DELETE FROM app_control_rules WHERE policy_id = ?", [policyID]);
      } finally {
        await db.end();
      }
      // JIT-provision the dex admin, then grant it admin, which holds the application control writes.
      await signInViaDex(page, ADMIN_EMAIL);
      await page.request.delete("/api/session");
      const promoteDB = await openDB();
      try {
        await promote(promoteDB, ADMIN_EMAIL, "admin");
      } finally {
        await promoteDB.end();
      }
    } finally {
      await ctx.close();
    }
  });

  // The rule this spec saves stays out of the specs that run after it, as policy-editor.spec.ts leaves none behind.
  test.afterAll(async () => {
    const db = await openDB();
    try {
      await db.query("DELETE FROM app_control_rules WHERE policy_id = ? AND identifier = ?", [policyID, IDENTIFIER]);
    } finally {
      await db.end();
    }
  });

  // spec:server-identity-authentication/reauthentication-is-required-for-destructive-actions/application-control-needs-a-fresh-session
  test("a stale session's new rule is kept through dex and saved only on the second save", async ({ browser }) => {
    const ctx = await browser.newContext();
    try {
      const page = await ctx.newPage();
      await signInViaDex(page, ADMIN_EMAIL);

      const ageDB = await openDB();
      try {
        await ageDB.query(
          `UPDATE sessions SET last_auth_at = NOW(6) - INTERVAL 1 HOUR
            WHERE user_id = (SELECT id FROM users WHERE email = ?)`,
          [ADMIN_EMAIL],
        );
      } finally {
        await ageDB.end();
      }

      await page.goto(`/ui/app-control/policies/${String(policyID)}`);
      await expect(page.getByRole("heading", { name: /default/i })).toBeVisible({ timeout: 15_000 });
      await page.getByRole("button", { name: /^add rule$/i }).click();
      await page.getByRole("textbox", { name: "Identifier", exact: true }).fill(IDENTIFIER);
      await page.getByRole("textbox", { name: /reason \(required for audit log\)/i }).fill(REASON);
      await page.getByRole("radio", { name: /detect/i }).check();

      const [refused] = await Promise.all([
        page.waitForResponse((r) => r.url().includes(`/policies/${String(policyID)}/rules`) && r.request().method() === "POST"),
        page.getByRole("button", { name: /save rule/i }).click(),
      ]);
      expect(refused.status()).toBe(403);
      expect(((await refused.json()) as { error: string }).error).toBe("reauth_required");

      const dialog = page.locator("dialog.reauth-dialog");
      await expect(dialog).toBeVisible({ timeout: 10_000 });
      await expect(dialog.getByText(/your unsaved changes on this page are kept/i)).toBeVisible();
      expect(await storedIdentifiers(policyID)).toEqual([]);

      await Promise.all([
        page.waitForURL(/localhost:5556\/dex/, { timeout: 30_000 }),
        dialog.getByRole("button", { name: /continue with single sign-on/i }).click(),
      ]);
      await page.locator('input[name="login"]').fill(ADMIN_EMAIL);
      await page.locator('input[name="password"]').fill(dexPassword);
      await page.getByRole("button", { name: /login/i }).click();
      await page.waitForURL(`**/ui/app-control/policies/${String(policyID)}`, { timeout: 30_000 });

      // The page reopens Add rule with the unsaved rule and the notice, and signing in stored nothing.
      await expect(page.getByText(/your unsaved changes were kept while you signed in again/i)).toBeVisible({ timeout: 15_000 });
      await expect(page.getByRole("textbox", { name: "Identifier", exact: true })).toHaveValue(IDENTIFIER);
      await expect(page.getByRole("textbox", { name: /reason \(required for audit log\)/i })).toHaveValue(REASON);
      expect(await storedIdentifiers(policyID)).toEqual([]);

      const [saved] = await Promise.all([
        page.waitForResponse((r) => r.url().includes(`/policies/${String(policyID)}/rules`) && r.request().method() === "POST"),
        page.getByRole("button", { name: /save rule/i }).click(),
      ]);
      expect(saved.status()).toBe(201);
      expect(await storedIdentifiers(policyID)).toEqual([IDENTIFIER]);
    } finally {
      await ctx.close();
    }
  });
});
