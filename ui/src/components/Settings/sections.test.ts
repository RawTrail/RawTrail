import { describe, expect, it } from "vitest";
import { PermissionAction } from "../../permissions-core";
import { offeredSections, settingsEntry } from "./sections";

const can = (allowed: readonly string[]) => (action: string) => allowed.includes(action);

describe("offeredSections and settingsEntry", () => {
  it("leave out the advanced sections while they are hidden", () => {
    const all = can(Object.values(PermissionAction));
    expect(offeredSections(all, false).map((s) => s.label)).toEqual(["Webhooks", "Data collection", "Users"]);
    expect(offeredSections(all, true)).toHaveLength(6);
    expect(settingsEntry(all, false)).toBe("/admin/settings/webhooks");
    expect(settingsEntry(all, true)).toBe("/admin/settings/sso");
  });

  it("still lead an operator whose every section is advanced into settings, where the switch is", () => {
    expect(settingsEntry(can([PermissionAction.SSOManage]), false)).toBe("/admin/settings/sso");
    expect(settingsEntry(can([]), false)).toBeNull();
  });
});
