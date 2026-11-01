import * as crypto from "node:crypto";
import { test, expect } from "../../fixtures/test";
import { openDB } from "../../fixtures/db";

// The first-run checklist (issue #1436) walks the approvals a Mac needs from its live agent health. The Mac is seeded rather than
// found: a host whose security extension macOS refused Full Disk Access, which the checklist must show red, and which then reports
// every approval made, which the open page must pick up without a reload. The test names the host with ?host= so it never depends on
// which host in the dev database reported last, and removes its rows afterwards.
const HOST_ID = `e2e-setup-${crypto.randomUUID()}`;
const HOSTNAME = "qa-setup-mbp";

const component = (type: string, status: string, reason: string) => ({ type, status, reason, message: "", last_transition_ns: 1 });

async function report(components: ReturnType<typeof component>[], eventCount: number) {
  const db = await openDB();
  try {
    const nowNs = Date.now() * 1_000_000;
    await db.query(
      `INSERT INTO hosts (host_id, event_count, last_seen_ns) VALUES (?, ?, ?)
       ON DUPLICATE KEY UPDATE event_count = VALUES(event_count), last_seen_ns = VALUES(last_seen_ns)`,
      [HOST_ID, eventCount, nowNs],
    );
    await db.query(
      `INSERT INTO host_health (host_id, overall_status, components, reported_at_ns) VALUES (?, 'unhealthy', ?, ?)
       ON DUPLICATE KEY UPDATE components = VALUES(components), reported_at_ns = VALUES(reported_at_ns)`,
      [HOST_ID, JSON.stringify(components), nowNs],
    );
  } finally {
    await db.end();
  }
}

test.describe("first-run checklist", () => {
  test.beforeEach(async () => {
    await report(
      [
        component("endpoint_security_extension", "unhealthy", "full_disk_access_missing"),
        component("network_extension", "unhealthy", "never_connected"),
      ],
      0,
    );
    const db = await openDB();
    try {
      await db.query(
        `INSERT INTO enrollments (host_id, hostname, agent_version, os_version, platform, source_ip)
           VALUES (?, ?, '0.9.0', '26.0', 'darwin', '127.0.0.1')`,
        [HOST_ID, HOSTNAME],
      );
    } finally {
      await db.end();
    }
  });

  test.afterEach(async () => {
    const db = await openDB();
    try {
      for (const table of ["agent_sessions", "host_health", "enrollments", "hosts"]) {
        await db.query(`DELETE FROM ${table} WHERE host_id = ?`, [HOST_ID]);
      }
    } finally {
      await db.end();
    }
  });

  // spec:web-ui/the-first-run-checklist-walks-the-approvals-from-live-health/a-refused-full-disk-access-grant-is-a-red-step
  // spec:web-ui/the-first-run-checklist-walks-the-approvals-from-live-health/the-checklist-follows-the-mac-as-it-is-set-up
  test("shows a refused grant red, then follows the Mac to done", async ({ signedInAdminShared: page }) => {
    await page.goto(`/ui/setup?host=${encodeURIComponent(HOST_ID)}`);
    await expect(page.getByRole("heading", { name: "Set up this Mac" })).toBeVisible();
    await expect(page.getByText(HOSTNAME)).toBeVisible();

    const steps = page.getByRole("list", { name: "Setup steps" }).getByRole("listitem");
    await expect(steps).toHaveCount(8);
    const fda = steps.filter({ hasText: "Give the security extension Full Disk Access" });
    await expect(fda).toHaveClass(/setup-checklist__step--missing/);
    await expect(fda).toContainText("Action needed");
    await expect(fda).toContainText("System Settings > Privacy & Security > Full Disk Access");
    // Red is drawn, not only named: the step's left edge is the alarm colour, unlike a waiting step's.
    const edge = (name: string) => steps.filter({ hasText: name }).evaluate((el) => getComputedStyle(el).borderLeftColor);
    expect(await edge("Give the security extension Full Disk Access")).not.toBe(await edge("Allow the network extension"));
    await expect(steps.filter({ hasText: "Allow the security extension" }).first()).toContainText("Done");

    // The Mac reports every approval made, its first event and its first session; the open page picks it up on its next read.
    await report(
      [
        component("endpoint_security_extension", "healthy", "activated"),
        component("network_extension", "healthy", "activated"),
        component("content_filter", "healthy", "activated"),
        component("dns_proxy", "healthy", "activated"),
        component("agent_full_disk_access", "healthy", "full_disk_access_granted"),
      ],
      1,
    );
    const db = await openDB();
    try {
      const nowNs = Date.now() * 1_000_000;
      await db.query(
        `INSERT INTO agent_sessions (host_id, agent_session_id, agent_name, agent_version, agent_pid, cwd, first_seen_ns, first_event_id,
           last_seen_ns) VALUES (?, ?, 'claude-code', '2.1.302', 40999, '/Users/qa/project', ?, ?, ?)`,
        [HOST_ID, crypto.randomUUID(), nowNs, `setup-${HOST_ID}`, nowNs],
      );
    } finally {
      await db.end();
    }

    await expect(page.getByText("Everything is set up.")).toBeVisible({ timeout: 20_000 });
    await expect(steps.filter({ hasText: "Done" })).toHaveCount(8);
    await page.getByRole("link", { name: "Go to Agent sessions" }).click();
    await expect(page).toHaveURL(/\/ui\/agent-sessions$/);
  });
});
