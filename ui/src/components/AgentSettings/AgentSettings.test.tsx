import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { AgentSettings } from "./AgentSettings";
import { PermissionsProvider } from "../../permissions";
import { PermissionAction } from "../../permissions-core";
import * as api from "../../api";

const makeSettings = (over: Partial<api.AgentSettings> = {}): api.AgentSettings => ({
  version: 0,
  capture_prompts: true,
  ...over,
});

// jsdom doesn't implement HTMLDialogElement.showModal/close; stub them so the reason modal renders.
beforeEach(() => {
  HTMLDialogElement.prototype.showModal = function showModal() {
    this.open = true;
  };
  HTMLDialogElement.prototype.close = function close() {
    this.open = false;
  };
});

afterEach(() => {
  vi.restoreAllMocks();
});

async function renderLoaded(
  settings: api.AgentSettings = makeSettings(),
  permissions: string[] = [PermissionAction.DetectionConfigRead, PermissionAction.DetectionConfigWrite],
) {
  vi.spyOn(api, "getAgentSettings").mockResolvedValue(settings);
  render(
    <PermissionsProvider permissions={permissions}>
      <AgentSettings />
    </PermissionsProvider>,
  );
  return screen.findByRole("switch", { name: "Capture prompts" });
}

function turnOffWithReason(reason: string) {
  fireEvent.click(screen.getByRole("switch", { name: "Capture prompts" }));
  fireEvent.change(screen.getByRole("textbox"), { target: { value: reason } });
  fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
}

// spec:web-ui/data-collection-settings-show-and-change-prompt-capture/the-page-says-plainly-that-prompts-are-captured
describe("AgentSettings", () => {
  it("says plainly that prompts are captured while capture is on", async () => {
    const toggle = await renderLoaded();
    expect(toggle).toBeChecked();
    expect(screen.getByText(/^Prompt capture is on\. Agents send the prompts people type/)).toBeVisible();
    expect(screen.getByText(/EDR_CAPTURE_PROMPTS=0/, { selector: "code" })).toBeVisible();
  });

  it("says capture is off, and who turned it off and when", async () => {
    const toggle = await renderLoaded(makeSettings({
      version: 3, capture_prompts: false, updated_at: "2026-10-29T06:00:00Z", updated_by: "usr_1", updated_by_label: "alice@example.com",
    }));
    expect(toggle).not.toBeChecked();
    expect(screen.getByText("Prompt capture is off. Agents do not send the prompts people type to AI coding agents.")).toBeVisible();
    expect(screen.getByText(/^Last changed .* by alice@example\.com\.$/)).toBeVisible();
  });

  // spec:web-ui/data-collection-settings-show-and-change-prompt-capture/turning-capture-off-asks-for-a-reason-and-reports-the-push
  it("turns capture off with a reason, against the version it loaded, and reports the push", async () => {
    await renderLoaded(makeSettings({ version: 2 }));
    const replace = vi.spyOn(api, "replaceAgentSettings").mockResolvedValue({
      settings: makeSettings({ version: 3, capture_prompts: false }), fanout_hosts: 4, fanout_failed: 1,
    });
    fireEvent.click(screen.getByRole("switch", { name: "Capture prompts" }));
    expect(screen.getByRole("heading", { name: "Turn prompt capture off?" })).toBeVisible();
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "works council" } });
    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));

    expect(await screen.findByText(/^Saved as version 3\. Queued for 3 of 4 enrolled hosts; the rest receive it within minutes\.$/))
      .toBeVisible();
    expect(replace).toHaveBeenCalledWith(false, "works council", 2);
    expect(screen.getByRole("switch", { name: "Capture prompts" })).not.toBeChecked();
    expect(screen.getByText(/^Prompt capture is off\./)).toBeVisible();
  });

  it("asks before turning capture back on", async () => {
    await renderLoaded(makeSettings({ version: 3, capture_prompts: false }));
    const replace = vi.spyOn(api, "replaceAgentSettings").mockResolvedValue({
      settings: makeSettings({ version: 4 }), fanout_hosts: 1, fanout_failed: 0,
    });
    fireEvent.click(screen.getByRole("switch", { name: "Capture prompts" }));
    expect(screen.getByRole("heading", { name: "Turn prompt capture on?" })).toBeVisible();
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "policy reviewed" } });
    fireEvent.click(screen.getByRole("button", { name: "Turn on" }));
    await waitFor(() => {
      expect(screen.getByRole("switch", { name: "Capture prompts" })).toBeChecked();
    });
    expect(replace).toHaveBeenCalledWith(true, "policy reviewed", 3);
  });

  it("leaves the settings alone when the reason dialog is cancelled", async () => {
    await renderLoaded();
    const replace = vi.spyOn(api, "replaceAgentSettings");
    fireEvent.click(screen.getByRole("switch", { name: "Capture prompts" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(replace).not.toHaveBeenCalled();
    expect(screen.getByRole("switch", { name: "Capture prompts" })).toBeChecked();
  });

  it("offers the latest settings when someone else changed them first", async () => {
    await renderLoaded(makeSettings({ version: 1 }));
    vi.spyOn(api, "replaceAgentSettings").mockRejectedValue(
      new api.DetectionConfigApiError("detection_config.conflict", "the agent settings were changed since they were read", 409),
    );
    turnOffWithReason("r");
    expect(await screen.findByRole("alert")).toHaveTextContent(/^Not changed: someone changed the agent settings/);

    vi.spyOn(api, "getAgentSettings").mockResolvedValue(makeSettings({ version: 2, capture_prompts: false }));
    fireEvent.click(screen.getByRole("button", { name: "Load the latest settings" }));
    await waitFor(() => {
      expect(screen.getByRole("switch", { name: "Capture prompts" })).not.toBeChecked();
    });
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("says why the server refused a change", async () => {
    await renderLoaded();
    vi.spyOn(api, "replaceAgentSettings").mockRejectedValue(
      new api.DetectionConfigApiError("detection_config.invalid_input", "capture_prompts is required", 400),
    );
    turnOffWithReason("r");
    expect(await screen.findByRole("alert")).toHaveTextContent("Not changed: capture_prompts is required");
    expect(screen.queryByRole("button", { name: "Load the latest settings" })).toBeNull();
    expect(screen.getByRole("switch", { name: "Capture prompts" })).toBeChecked();
  });

  it("says the setting is unchanged when the operator does not confirm their identity", async () => {
    const read = vi.spyOn(api, "getAgentSettings");
    await renderLoaded();
    vi.spyOn(api, "replaceAgentSettings").mockRejectedValue(
      new api.ReauthRequiredError({ authMethod: "oidc", reauthURL: "https://idp.example/reauth" }),
    );
    turnOffWithReason("r");
    const identity = await screen.findByRole("dialog", { name: "Confirm your identity" });
    fireEvent.click(within(identity).getByRole("button", { name: "Cancel" }));
    expect(await screen.findByText("Not changed: confirm your identity to change this setting.")).toBeVisible();
    expect(read).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("switch", { name: "Capture prompts" })).toBeChecked();
  });

  // A change whose answer never arrived may still have been stored, so the page reads the setting again rather than say it did not.
  it("shows what the server holds when a change's outcome is unknown", async () => {
    await renderLoaded(makeSettings({ version: 1 }));
    vi.spyOn(api, "replaceAgentSettings").mockRejectedValue(new TypeError("Failed to fetch"));
    vi.spyOn(api, "getAgentSettings").mockResolvedValue(makeSettings({ version: 2, capture_prompts: false }));
    turnOffWithReason("r");
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Not confirmed: Failed to fetch. The setting shown is what the server holds now.",
    );
    expect(screen.getByRole("switch", { name: "Capture prompts" })).not.toBeChecked();
  });

  it("offers to try again when the outcome is unknown and the setting cannot be read either", async () => {
    await renderLoaded();
    vi.spyOn(api, "replaceAgentSettings").mockRejectedValue(new TypeError("Failed to fetch"));
    vi.spyOn(api, "getAgentSettings").mockRejectedValue(new Error("network down"));
    turnOffWithReason("r");
    expect(await screen.findByText(/^The latest settings could not be loaded: network down/)).toBeVisible();
    expect(screen.getByRole("button", { name: "Load the latest settings" })).toBeVisible();
  });

  it("keeps the page and the offer to try again when the latest settings cannot be loaded", async () => {
    await renderLoaded(makeSettings({ version: 1 }));
    vi.spyOn(api, "replaceAgentSettings").mockRejectedValue(
      new api.DetectionConfigApiError("detection_config.conflict", "the agent settings were changed since they were read", 409),
    );
    turnOffWithReason("r");
    await screen.findByRole("button", { name: "Load the latest settings" });
    vi.spyOn(api, "getAgentSettings").mockRejectedValue(new Error("network down"));
    fireEvent.click(screen.getByRole("button", { name: "Load the latest settings" }));
    expect(await screen.findByText(/^The latest settings could not be loaded: network down/)).toBeVisible();
    expect(screen.getByRole("button", { name: "Load the latest settings" })).toBeVisible();
    expect(screen.getByRole("switch", { name: "Capture prompts" })).toBeVisible();
  });

  it("shows the setting but cannot change it without detection_config.write", async () => {
    const toggle = await renderLoaded(makeSettings(), [PermissionAction.DetectionConfigRead]);
    expect(toggle).toBeDisabled();
  });

  it("says when the settings could not be loaded", async () => {
    vi.spyOn(api, "getAgentSettings").mockRejectedValue(new Error("boom"));
    render(
      <PermissionsProvider permissions={[PermissionAction.DetectionConfigRead]}>
        <AgentSettings />
      </PermissionsProvider>,
    );
    expect(await screen.findByRole("alert")).toHaveTextContent("The agent settings could not be loaded: boom");
  });
});
