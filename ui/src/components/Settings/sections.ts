import { PermissionAction } from "../../permissions-core";

// The Admin settings sections, in the order the sub-navigation shows them, each with the permission that opens it.
//
// Shared with AccountMenu rather than held in SettingsLayout, because the menu's "Admin settings" link is the only way into this
// area: a section whose permission the menu does not know about is a page nobody can navigate to. That is not hypothetical. The
// link was gated on `sso.manage` alone, so the moment Containment arrived with `containment_config.read`, the senior analysts it
// was added for could reach it only by typing the URL.
//
// An advanced section is a fleet's concern (issue #1436): a laptop install offers it only once "Show advanced settings" is on. Its page
// stays reachable by its address either way; the switch decides what the navigation offers, not what exists.
export const SETTINGS_SECTIONS = [
  { to: "/admin/settings/sso", label: "Single sign-on", action: PermissionAction.SSOManage, advanced: true },
  { to: "/admin/settings/webhooks", label: "Webhooks", action: PermissionAction.WebhookManage, advanced: false },
  { to: "/admin/settings/containment", label: "Containment", action: PermissionAction.ContainmentConfigRead, advanced: true },
  { to: "/admin/settings/data-collection", label: "Data collection", action: PermissionAction.DetectionConfigRead, advanced: false },
  { to: "/admin/settings/users", label: "Users", action: PermissionAction.UserRead, advanced: false },
  { to: "/admin/settings/service-accounts", label: "Service accounts", action: PermissionAction.ServiceAccountRead, advanced: true },
] as const;

// offeredSections are the sections the navigation offers: those the operator can open, less the advanced ones while they are hidden.
export function offeredSections(can: (action: string) => boolean, showAdvanced: boolean) {
  return SETTINGS_SECTIONS.filter((s) => can(s.action) && (showAdvanced || !s.advanced));
}

// settingsEntry is where "Admin settings" should land an operator: the first section offered, else the first they can open (so an
// operator whose every section is advanced still reaches the switch that shows them), or null when they can open none, in which case
// the entry is not offered. The server chokepoint remains the authority (ADR-0012); this decides what to offer, not what is allowed.
export function settingsEntry(can: (action: string) => boolean, showAdvanced: boolean): string | null {
  const offered = SETTINGS_SECTIONS.find((s) => can(s.action) && (showAdvanced || !s.advanced));
  return (offered ?? SETTINGS_SECTIONS.find((s) => can(s.action)))?.to ?? null;
}
