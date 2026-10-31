import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { CredentialGuard } from "./CredentialGuard";
import * as api from "../../api";
import { persistReauthDrafts, readReauthDraft, setReauthDraftUser } from "../../reauthDraft";
import type { CredentialGuardPolicy, ProtectedCredential, ReplaceCredentialGuardResult } from "../../api";

const SSH: ProtectedCredential = {
  kind: "ssh_private_key",
  path: ".ssh/id_",
  match: "prefix",
  owners: [{ signing_id: "com.apple.ssh" }],
  default: true,
};
const GH: ProtectedCredential = {
  kind: "github_cli_token",
  path: ".config/gh/hosts.yml",
  match: "literal",
  owners: [{ team_id: "VEKTX9H2N7" }],
  default: true,
};

const makePolicy = (over: Partial<CredentialGuardPolicy> = {}): CredentialGuardPolicy => ({
  version: 2,
  mode: "detect",
  credentials: [SSH, GH],
  updated_at: "2026-10-30T12:00:00Z",
  updated_by: "usr_1",
  updated_by_label: "alice@example.com",
  ...over,
});

const makeResult = (policy: CredentialGuardPolicy): ReplaceCredentialGuardResult => ({ policy, fanout_hosts: 3, fanout_failed: 0 });

// jsdom doesn't implement HTMLDialogElement.showModal/close; stub them so the reason modal renders.
beforeEach(() => {
  setReauthDraftUser(1);
  HTMLDialogElement.prototype.showModal = function showModal() {
    this.open = true;
  };
  HTMLDialogElement.prototype.close = function close() {
    this.open = false;
  };
});

afterEach(() => {
  vi.restoreAllMocks();
  sessionStorage.clear();
  setReauthDraftUser(null);
});

async function renderLoaded(policy: CredentialGuardPolicy = makePolicy(), canWrite = true) {
  vi.spyOn(api, "getCredentialGuard").mockResolvedValue(policy);
  render(<CredentialGuard canWrite={canWrite} />);
  await screen.findByLabelText("Mode");
}

function rows(): string[] {
  return screen
    .queryAllByRole("row")
    .slice(1)
    .map((r) => r.textContent);
}

function fillForm(kind: string, path: string, match: "literal" | "prefix", owners: string) {
  fireEvent.change(screen.getByLabelText("Credential"), { target: { value: kind } });
  fireEvent.change(screen.getByLabelText("Path in each home"), { target: { value: path } });
  fireEvent.change(screen.getByLabelText("Covers"), { target: { value: match } });
  fireEvent.change(screen.getByLabelText("Tools allowed to read it"), { target: { value: owners } });
}

async function saveWithReason(reason: string) {
  fireEvent.click(screen.getByRole("button", { name: "Save and push to hosts" }));
  fireEvent.change(await screen.findByLabelText(/required for audit log/i), { target: { value: reason } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
}

describe("CredentialGuard", () => {
  // spec:web-ui/the-credential-guard-is-edited-in-detection-tuning/the-console-shows-the-policy-in-plain-words
  it("shows the mode and each credential in plain words, the built-ins marked, and the last change", async () => {
    await renderLoaded();

    expect(screen.getByLabelText("Mode")).toHaveValue("detect");
    expect(rows()).toEqual([
      "SSH private keysBuilt in~/.ssh/id_Files whose names start with itApple's com.apple.sshEditRemove",
      "GitHub CLI tokenBuilt in~/.config/gh/hosts.ymlThis fileAny tool signed by team VEKTX9H2N7EditRemove",
    ]);
    expect(screen.getByText(/Last saved .* by alice@example\.com\./)).toBeVisible();
    expect(screen.getByRole("button", { name: "Save and push to hosts" })).toBeDisabled();
  });

  it("offers no editing to an operator without write permission, and says when the guard protects nothing", async () => {
    await renderLoaded(makePolicy({ version: 0, credentials: [], updated_at: undefined }), false);

    expect(screen.getByLabelText("Mode")).toBeDisabled();
    expect(screen.getByText(/No credentials protected/)).toBeVisible();
    expect(screen.queryByRole("button", { name: "Save and push to hosts" })).toBeNull();
    expect(screen.queryByLabelText("Path in each home")).toBeNull();
    expect(screen.queryByText(/Last saved/)).toBeNull();
  });

  // spec:web-ui/the-credential-guard-is-edited-in-detection-tuning/a-changed-policy-is-saved-with-a-reason-and-pushed
  it("saves a new mode, an added credential and edited tools with the reason, and reports the push", async () => {
    await renderLoaded();
    fireEvent.change(screen.getByLabelText("Mode"), { target: { value: "protect" } });
    fillForm("aws_credentials", ".aws/credentials", "literal", "94KV3E626L");
    fireEvent.click(screen.getByRole("button", { name: "Add credential" }));
    expect(screen.getByLabelText("Path in each home")).toHaveValue("");

    // Editing takes the entry out of the list into the form, with its tools as they are typed, and adding puts it back.
    fireEvent.click(screen.getByRole("button", { name: "Edit ~/.config/gh/hosts.yml" }));
    expect(screen.getByLabelText("Tools allowed to read it")).toHaveValue("VEKTX9H2N7");
    expect(screen.getByRole("button", { name: "Save and push to hosts" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Tools allowed to read it"), { target: { value: "VEKTX9H2N7, ABCDE12345:gh" } });
    fireEvent.click(screen.getByRole("button", { name: "Add credential" }));

    const sent: ProtectedCredential[] = [
      SSH,
      { kind: "aws_credentials", path: ".aws/credentials", match: "literal", owners: [{ team_id: "94KV3E626L" }] },
      { kind: "github_cli_token", path: ".config/gh/hosts.yml", match: "literal", owners: [{ team_id: "VEKTX9H2N7" }, { team_id: "ABCDE12345", signing_id: "gh" }] },
    ];
    const replace = vi
      .spyOn(api, "replaceCredentialGuard")
      .mockResolvedValue(makeResult(makePolicy({ version: 3, mode: "protect", credentials: sent, updated_by_label: "bob@example.com" })));
    await saveWithReason("refuse agent reads, and allow our signed gh");

    await waitFor(() => {
      expect(replace).toHaveBeenCalledWith("protect", sent, "refuse agent reads, and allow our signed gh", 2);
    });
    expect(await screen.findByRole("status")).toHaveTextContent("Saved as version 3.");
    expect(screen.getByLabelText("Mode")).toHaveValue("protect");
    expect(screen.getByText(/Last saved .* by bob@example\.com\./)).toBeVisible();
    expect(screen.getByRole("button", { name: "Save and push to hosts" })).toBeDisabled();
  });

  it("refuses to add a credential with no path, or a path already listed", async () => {
    await renderLoaded();
    const add = screen.getByRole("button", { name: "Add credential" });
    expect(add).toBeDisabled();
    fillForm("ssh_private_key", ".ssh/id_", "prefix", "");
    expect(add).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Path in each home"), { target: { value: ".ssh/deploy_key" } });
    expect(add).toBeEnabled();
  });

  it("shows the server's refusal as written, and offers the latest policy after a conflicting change", async () => {
    await renderLoaded();
    fireEvent.click(screen.getByRole("button", { name: "Remove ~/.ssh/id_" }));
    const replace = vi
      .spyOn(api, "replaceCredentialGuard")
      .mockRejectedValueOnce(new api.DetectionConfigApiError("detection_config.invalid_input", "credentials[0]: kind must be one of", 400))
      .mockRejectedValueOnce(new api.DetectionConfigApiError("detection_config.conflict", "out of date", 409));
    await saveWithReason("drop keys");
    expect(await screen.findByRole("alert")).toHaveTextContent("Not saved: credentials[0]: kind must be one of");
    expect(rows()).toHaveLength(1);

    await saveWithReason("drop keys");
    await waitFor(() => {
      expect(replace).toHaveBeenCalledTimes(2);
    });
    vi.spyOn(api, "getCredentialGuard").mockResolvedValue(makePolicy({ version: 4, mode: "off" }));
    fireEvent.click(await screen.findByRole("button", { name: /Load the latest policy/ }));
    await waitFor(() => {
      expect(screen.getByLabelText("Mode")).toHaveValue("off");
    });
    expect(rows()).toHaveLength(2);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  // spec:server-identity-authentication/reauthentication-is-required-for-destructive-actions/fleet-wide-change-needs-a-fresh-session
  it("asks the operator to confirm their identity when the save needs a fresh sign-in", async () => {
    await renderLoaded();
    vi.spyOn(api, "replaceCredentialGuard").mockRejectedValue(
      new api.ReauthRequiredError({ authMethod: "oidc", reauthURL: "https://idp.example/reauth" }),
    );
    fireEvent.change(screen.getByLabelText("Mode"), { target: { value: "off" } });
    await saveWithReason("pause the guard");

    expect(await screen.findByRole("heading", { name: "Confirm your identity" })).toBeVisible();
    expect(screen.queryByText(/Not saved/)).toBeNull();
  });

  it("discards the mode, the list and the form back to the stored policy", async () => {
    await renderLoaded();
    fireEvent.change(screen.getByLabelText("Mode"), { target: { value: "off" } });
    fireEvent.click(screen.getByRole("button", { name: "Remove ~/.ssh/id_" }));
    fireEvent.change(screen.getByLabelText("Path in each home"), { target: { value: ".netrc" } });
    fireEvent.click(screen.getByRole("button", { name: "Discard changes" }));

    expect(screen.getByLabelText("Mode")).toHaveValue("detect");
    expect(rows()).toHaveLength(2);
    expect(screen.getByLabelText("Path in each home")).toHaveValue("");
  });

  it("says the policy could not be loaded", async () => {
    vi.spyOn(api, "getCredentialGuard").mockRejectedValue(new Error("network down"));
    render(<CredentialGuard canWrite />);
    expect(await screen.findByText("The credential guard policy could not be loaded: network down")).toBeVisible();
  });
});

describe("CredentialGuard across a reauthentication", () => {
  // spec:web-ui/unsaved-work-survives-a-reauthentication/unsaved-work-is-restored-and-not-saved-for-the-operator
  it("puts back the mode, the list and the credential being typed, and saves against the version it was edited from", async () => {
    await renderLoaded();
    fireEvent.change(screen.getByLabelText("Mode"), { target: { value: "protect" } });
    fireEvent.click(screen.getByRole("button", { name: "Remove ~/.ssh/id_" }));
    fillForm("netrc", ".netrc", "literal", "com.apple.curl");
    expect(persistReauthDrafts()).toBe(true);
    cleanup();

    await renderLoaded(makePolicy({ version: 5 }));
    expect(await screen.findByText(/changed by someone else while you were away/)).toBeVisible();
    expect(screen.getByLabelText("Mode")).toHaveValue("protect");
    expect(rows()).toEqual([expect.stringContaining("GitHub CLI token")]);
    expect(screen.getByLabelText("Credential")).toHaveValue("netrc");
    expect(screen.getByLabelText("Tools allowed to read it")).toHaveValue("com.apple.curl");
    expect(readReauthDraft("credential-guard")).toBeUndefined();

    fireEvent.click(screen.getByRole("button", { name: "Add credential" }));
    const replace = vi.spyOn(api, "replaceCredentialGuard").mockResolvedValue(makeResult(makePolicy({ version: 6 })));
    await saveWithReason("protect");
    await waitFor(() => {
      expect(replace).toHaveBeenCalledWith(
        "protect",
        [GH, { kind: "netrc", path: ".netrc", match: "literal", owners: [{ signing_id: "com.apple.curl" }] }],
        "protect",
        2,
      );
    });
  });

  it("restores nothing when the section was clean", async () => {
    await renderLoaded();
    persistReauthDrafts();
    expect(readReauthDraft("credential-guard")).toBeUndefined();
  });
});
