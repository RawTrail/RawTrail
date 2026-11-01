import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import type { ReactNode } from "react";
import { SettingsLayout } from "./SettingsLayout";
import { PermissionsProvider } from "../../permissions";
import { PermissionAction } from "../../permissions-core";
import { ProfileProvider } from "../../profile";
import { ADVANCED_KEY, type Profile } from "../../profile-core";

function renderLayout(permissions: string[] | undefined, path = "/admin/settings/sso", profile: Profile = "server") {
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <MemoryRouter initialEntries={[path]}>
        <PermissionsProvider permissions={permissions}>
          <ProfileProvider profile={profile}>{children}</ProfileProvider>
        </PermissionsProvider>
      </MemoryRouter>
    );
  }
  render(<SettingsLayout><div>section content</div></SettingsLayout>, { wrapper: Wrapper });
}

afterEach(() => { vi.restoreAllMocks(); });

describe("SettingsLayout", () => {
  it("renders the section content and all sub-nav links when permitted", () => {
    renderLayout([PermissionAction.SSOManage, PermissionAction.UserRead, PermissionAction.ServiceAccountRead]);
    expect(screen.getByText("section content")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Single sign-on" })).toHaveAttribute("href", "/admin/settings/sso");
    expect(screen.getByRole("link", { name: "Users" })).toHaveAttribute("href", "/admin/settings/users");
    expect(screen.getByRole("link", { name: "Service accounts" })).toHaveAttribute("href", "/admin/settings/service-accounts");
  });

  it("omits a section the operator lacks permission for", () => {
    renderLayout([PermissionAction.ServiceAccountRead]);
    expect(screen.queryByRole("link", { name: "Single sign-on" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Users" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Service accounts" })).toBeInTheDocument();
  });

  it("marks the active section with aria-current", () => {
    renderLayout([PermissionAction.SSOManage, PermissionAction.ServiceAccountRead], "/admin/settings/service-accounts");
    expect(screen.getByRole("link", { name: "Service accounts" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "Single sign-on" })).not.toHaveAttribute("aria-current");
  });

  // spec:web-ui/the-settings-area-manages-webhook-destinations/the-webhooks-section-is-hidden-without-the-manage-grant
  it("shows the Webhooks section only with webhook.manage", () => {
    renderLayout([PermissionAction.WebhookManage]);
    expect(screen.getByRole("link", { name: "Webhooks" })).toHaveAttribute("href", "/admin/settings/webhooks");
  });

  it("hides the Webhooks section without webhook.manage", () => {
    renderLayout([PermissionAction.ServiceAccountRead]);
    expect(screen.queryByRole("link", { name: "Webhooks" })).not.toBeInTheDocument();
  });

  describe("on a laptop install", () => {
    const all = [PermissionAction.SSOManage, PermissionAction.WebhookManage, PermissionAction.ContainmentConfigRead,
      PermissionAction.DetectionConfigRead, PermissionAction.UserRead, PermissionAction.ServiceAccountRead];
    const fleet = ["Single sign-on", "Containment", "Service accounts"];
    beforeEach(() => { localStorage.removeItem(ADVANCED_KEY); });

    // spec:web-ui/a-laptop-install-has-personal-defaults/fleet-settings-are-behind-a-switch
    it("offers fleet settings only once Show advanced settings is on, and remembers the choice", () => {
      renderLayout(all, "/admin/settings/data-collection", "laptop");
      for (const name of fleet) expect(screen.queryByRole("link", { name })).toBeNull();
      expect(screen.getByRole("link", { name: "Data collection" })).toBeVisible();
      const toggle = screen.getByRole("switch", { name: "Show advanced settings" });
      expect(toggle).not.toBeChecked();
      fireEvent.click(toggle);
      for (const name of fleet) expect(screen.getByRole("link", { name })).toBeVisible();
      expect(localStorage.getItem(ADVANCED_KEY)).toBe("1");
    });

    it("starts with fleet settings shown when this browser chose to before", () => {
      localStorage.setItem(ADVANCED_KEY, "1");
      renderLayout(all, "/admin/settings/data-collection", "laptop");
      for (const name of fleet) expect(screen.getByRole("link", { name })).toBeVisible();
    });

    it("works without storage, starting with fleet settings hidden", () => {
      vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("blocked"); });
      vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("blocked"); });
      renderLayout(all, "/admin/settings/data-collection", "laptop");
      expect(screen.queryByRole("link", { name: "Single sign-on" })).toBeNull();
      fireEvent.click(screen.getByRole("switch", { name: "Show advanced settings" }));
      expect(screen.getByRole("link", { name: "Single sign-on" })).toBeVisible();
    });
  });

  it("offers every section and no switch on a server", () => {
    renderLayout([PermissionAction.SSOManage, PermissionAction.ContainmentConfigRead], "/admin/settings/sso", "server");
    expect(screen.getByRole("link", { name: "Single sign-on" })).toBeVisible();
    expect(screen.getByRole("link", { name: "Containment" })).toBeVisible();
    expect(screen.queryByRole("switch", { name: "Show advanced settings" })).toBeNull();
  });
});
