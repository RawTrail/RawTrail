import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { StrictMode } from "react";
import { MemoryRouter, Route, Routes } from "react-router";
import { AddRuleModal } from "./AddRuleModal";
import { EditRuleModal } from "./EditRuleModal";
import { PasteManyModal } from "./PasteManyModal";
import { PolicyDetail } from "./PolicyDetail";
import * as api from "../../api";
import { PermissionAction, PermissionsContext } from "../../permissions-core";
import { persistReauthDrafts, readReauthDraft, setReauthDraftUser } from "../../reauthDraft";
import type { ApplicationControlPolicy, ApplicationControlRule } from "../../types";

// An OIDC reauthentication leaves the page, so the application control dialogs' unsaved work is kept across it, the page reopens the
// dialog the work belongs to, and the work is put back without being saved (issue #1113).

const makeRule = (over: Partial<ApplicationControlRule> = {}): ApplicationControlRule => ({
  id: 7,
  policy_id: 3,
  rule_type: "BINARY",
  identifier: "a".repeat(64),
  action: "BLOCK",
  enforcement: "PROTECT",
  enabled: true,
  severity: "high",
  source: "admin",
  custom_msg: "",
  custom_url: "",
  comment: "",
  created_at: "2026-05-14T00:00:00Z",
  updated_at: "2026-05-14T00:00:00Z",
  created_by: "user:1",
  ...over,
});

const makePolicy = (over: Partial<ApplicationControlPolicy> = {}): ApplicationControlPolicy => ({
  id: 3,
  name: "Default",
  description: "fixture",
  version: 5,
  default_action: "NONE",
  created_at: "2026-05-14T00:00:00Z",
  updated_at: "2026-05-14T00:00:00Z",
  created_by: "system",
  updated_by: "user:1",
  assignment_count: 1,
  rule_count: 1,
  rules: [makeRule()],
  ...over,
});

const EVERY_RULE_ACTION = [
  PermissionAction.AppControlRead,
  PermissionAction.AppControlRuleCreate,
  PermissionAction.AppControlRuleUpdate,
  PermissionAction.AppControlRuleDelete,
  PermissionAction.AppControlRuleBulkUpsert,
];

const noop = () => undefined;
const RESTORED = /Your unsaved changes were kept while you signed in again/;
const hex = "b".repeat(64);

beforeEach(() => {
  setReauthDraftUser(1);
  vi.spyOn(api, "listDetectionRuleMatchCounts").mockResolvedValue({ counts: [], days: 7 });
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

function saveDraft(key: string, value: unknown) {
  sessionStorage.setItem(`rawtrail:reauth-draft:${key}`, JSON.stringify({ savedAt: Date.now(), path: "/", userId: 1, value }));
}

describe("AddRuleModal across a reauthentication", () => {
  // spec:web-ui/unsaved-work-survives-a-reauthentication/unsaved-work-is-restored-and-not-saved-for-the-operator
  it("puts back what was typed, says so, and creates nothing", () => {
    const first = render(<AddRuleModal open policyID={3} onClose={noop} onCreated={noop} />);
    fireEvent.change(screen.getByLabelText(/identifier/i), { target: { value: hex } });
    fireEvent.change(screen.getByLabelText(/reason/i), { target: { value: "block the unsigned build" } });
    fireEvent.click(screen.getByRole("radio", { name: /protect/i }));
    expect(persistReauthDrafts()).toBe(true);
    first.unmount();

    const create = vi.spyOn(api, "createAppControlRule");
    render(<AddRuleModal open policyID={3} onClose={noop} onCreated={noop} />);

    expect(screen.getByText(RESTORED)).toBeVisible();
    expect(screen.getByLabelText(/identifier/i)).toHaveValue(hex);
    expect(screen.getByLabelText(/reason/i)).toHaveValue("block the unsigned build");
    expect(screen.getByRole("radio", { name: /protect/i })).toBeChecked();
    expect(create).not.toHaveBeenCalled();
    expect(readReauthDraft("appcontrol-add-rule:3")).toBeUndefined();
  });

  it("resets as it always did on a later open, rather than restoring again", () => {
    const first = render(<AddRuleModal open policyID={3} onClose={noop} onCreated={noop} />);
    fireEvent.change(screen.getByLabelText(/identifier/i), { target: { value: hex } });
    persistReauthDrafts();
    first.unmount();

    const view = render(<AddRuleModal open policyID={3} onClose={noop} onCreated={noop} />);
    expect(screen.getByLabelText(/identifier/i)).toHaveValue(hex);
    view.rerender(<AddRuleModal open={false} policyID={3} onClose={noop} onCreated={noop} />);
    view.rerender(<AddRuleModal open policyID={3} onClose={noop} onCreated={noop} />);

    expect(screen.getByLabelText(/identifier/i)).toHaveValue("");
    expect(screen.queryByText(RESTORED)).toBeNull();
  });

  it("keeps nothing when the dialog is closed or untouched", () => {
    const view = render(<AddRuleModal open policyID={3} onClose={noop} onCreated={noop} />);
    persistReauthDrafts();
    expect(readReauthDraft("appcontrol-add-rule:3")).toBeUndefined();
    fireEvent.change(screen.getByLabelText(/identifier/i), { target: { value: hex } });
    view.rerender(<AddRuleModal open={false} policyID={3} onClose={noop} onCreated={noop} />);
    persistReauthDrafts();
    expect(readReauthDraft("appcontrol-add-rule:3")).toBeUndefined();
  });

  // spec:web-ui/unsaved-work-survives-a-reauthentication/saved-work-is-not-restored-when-it-is-stale-or-from-another-page
  it.each([
    ["a null payload", null],
    ["a field that is not text", { ruleType: "BINARY", identifier: 1, enforcement: null, severity: "medium", customMsg: "", customURL: "", reason: "" }],
    ["an enforcement it does not know", { ruleType: "BINARY", identifier: "x", enforcement: "BLOCK", severity: "medium", customMsg: "", customURL: "", reason: "" }],
  ])("ignores %s under a valid envelope, and removes it", (_name, payload) => {
    saveDraft("appcontrol-add-rule:3", payload);

    render(<AddRuleModal open policyID={3} onClose={noop} onCreated={noop} />);

    expect(screen.queryByText(RESTORED)).toBeNull();
    expect(screen.getByLabelText(/identifier/i)).toHaveValue("");
    expect(sessionStorage.getItem("rawtrail:reauth-draft:appcontrol-add-rule:3")).toBeNull();
  });

  it("does not restore work another policy's dialog saved", () => {
    const first = render(<AddRuleModal open policyID={3} onClose={noop} onCreated={noop} />);
    fireEvent.change(screen.getByLabelText(/identifier/i), { target: { value: hex } });
    persistReauthDrafts();
    first.unmount();

    render(<AddRuleModal open policyID={4} onClose={noop} onCreated={noop} />);

    expect(screen.getByLabelText(/identifier/i)).toHaveValue("");
  });
});

describe("EditRuleModal across a reauthentication", () => {
  // spec:web-ui/unsaved-work-survives-a-reauthentication/unsaved-work-is-restored-and-not-saved-for-the-operator
  it("puts back the edits and the reason, says so, and saves nothing", () => {
    const rule = makeRule();
    const first = render(<EditRuleModal open rule={rule} onClose={noop} onSaved={noop} />);
    fireEvent.change(screen.getByLabelText(/severity/i), { target: { value: "critical" } });
    fireEvent.change(screen.getByLabelText(/custom message/i), { target: { value: "ask security" } });
    fireEvent.change(screen.getByLabelText(/reason/i), { target: { value: "raised after the incident" } });
    expect(persistReauthDrafts()).toBe(true);
    first.unmount();

    const update = vi.spyOn(api, "updateAppControlRule");
    render(<EditRuleModal open rule={rule} onClose={noop} onSaved={noop} />);

    expect(screen.getByText(RESTORED)).toBeVisible();
    expect(screen.getByLabelText(/severity/i)).toHaveValue("critical");
    expect(screen.getByLabelText(/custom message/i)).toHaveValue("ask security");
    expect(screen.getByLabelText(/reason/i)).toHaveValue("raised after the incident");
    expect(update).not.toHaveBeenCalled();
    expect(readReauthDraft("appcontrol-edit-rule:7")).toBeUndefined();
  });

  it("does not put one rule's edits into another rule's dialog", () => {
    const first = render(<EditRuleModal open rule={makeRule({ id: 7 })} onClose={noop} onSaved={noop} />);
    fireEvent.change(screen.getByLabelText(/reason/i), { target: { value: "only for seven" } });
    persistReauthDrafts();
    first.unmount();

    render(<EditRuleModal open rule={makeRule({ id: 8 })} onClose={noop} onSaved={noop} />);

    expect(screen.queryByText(RESTORED)).toBeNull();
    expect(screen.getByLabelText(/reason/i)).toHaveValue("");
  });

  // spec:web-ui/unsaved-work-survives-a-reauthentication/saved-work-is-not-restored-when-it-is-stale-or-from-another-page
  it.each([
    ["a null payload", null],
    ["a field missing", { severity: "high", customMsg: "", customURL: "", comment: "" }],
  ])("ignores %s under a valid envelope, and removes it", (_name, payload) => {
    saveDraft("appcontrol-edit-rule:7", payload);

    render(<EditRuleModal open rule={makeRule()} onClose={noop} onSaved={noop} />);

    expect(screen.queryByText(RESTORED)).toBeNull();
    expect(sessionStorage.getItem("rawtrail:reauth-draft:appcontrol-edit-rule:7")).toBeNull();
  });
});

describe("EditRuleModal reconciles restored edits with the rule as it is now", () => {
  // spec:web-ui/unsaved-work-survives-a-reauthentication/a-restored-edit-keeps-what-someone-else-changed-meanwhile
  it("puts back only what the operator changed, so a field someone else changed meanwhile is not written back over", async () => {
    const before = makeRule({ severity: "high", custom_msg: "ask security" });
    const first = render(<EditRuleModal open rule={before} onClose={noop} onSaved={noop} />);
    fireEvent.change(screen.getByLabelText(/severity/i), { target: { value: "critical" } });
    fireEvent.change(screen.getByLabelText(/reason/i), { target: { value: "raised after the incident" } });
    expect(persistReauthDrafts()).toBe(true);
    first.unmount();

    // While the operator signed in again, someone else changed the custom message.
    const now = makeRule({ severity: "high", custom_msg: "ask the helpdesk" });
    const update = vi.spyOn(api, "updateAppControlRule").mockResolvedValue(now);
    render(<EditRuleModal open rule={now} onClose={noop} onSaved={noop} />);

    expect(screen.getByLabelText(/severity/i)).toHaveValue("critical");
    expect(screen.getByLabelText(/custom message/i)).toHaveValue("ask the helpdesk");
    fireEvent.click(screen.getByRole("button", { name: /save changes/i }));

    await waitFor(() => {
      expect(update).toHaveBeenCalledWith(7, { severity: "critical", reason: "raised after the incident" });
    });
  });

  it("lets the operator's own edit of a field win over the rule's current value", () => {
    const first = render(<EditRuleModal open rule={makeRule({ custom_msg: "old" })} onClose={noop} onSaved={noop} />);
    fireEvent.change(screen.getByLabelText(/custom message/i), { target: { value: "mine" } });
    persistReauthDrafts();
    first.unmount();

    render(<EditRuleModal open rule={makeRule({ custom_msg: "theirs" })} onClose={noop} onSaved={noop} />);

    expect(screen.getByLabelText(/custom message/i)).toHaveValue("mine");
  });
});

describe("PasteManyModal across a reauthentication", () => {
  // spec:web-ui/unsaved-work-survives-a-reauthentication/unsaved-work-is-restored-and-not-saved-for-the-operator
  it("puts back the parsed rows, the choices and the reason, says so, and saves nothing", () => {
    const first = render(<PasteManyModal open policyID={3} onClose={noop} onUpserted={noop} />);
    fireEvent.change(screen.getByLabelText(/identifiers/i), { target: { value: `${hex}\nEQHXZ8M8AV` } });
    fireEvent.click(screen.getByRole("button", { name: /parse/i }));
    fireEvent.click(screen.getByRole("radio", { name: /detect/i }));
    fireEvent.change(screen.getByLabelText(/reason/i), { target: { value: "bulk import from the allowlist" } });
    expect(persistReauthDrafts()).toBe(true);
    first.unmount();

    const bulk = vi.spyOn(api, "bulkUpsertAppControlRules");
    render(<PasteManyModal open policyID={3} onClose={noop} onUpserted={noop} />);

    expect(screen.getByText(RESTORED)).toBeVisible();
    const types = screen.getAllByRole<HTMLSelectElement>("combobox", { name: /^type for row/i });
    expect(types.map((t) => t.value)).toEqual(["BINARY", "TEAMID"]);
    expect(screen.getByRole("radio", { name: /detect/i })).toBeChecked();
    expect(screen.getByLabelText(/reason/i)).toHaveValue("bulk import from the allowlist");
    expect(bulk).not.toHaveBeenCalled();
    expect(readReauthDraft("appcontrol-paste-many:3")).toBeUndefined();
  });

  it("puts back an unparsed paste in the paste phase", () => {
    const first = render(<PasteManyModal open policyID={3} onClose={noop} onUpserted={noop} />);
    fireEvent.change(screen.getByLabelText(/identifiers/i), { target: { value: "half a list" } });
    persistReauthDrafts();
    first.unmount();

    render(<PasteManyModal open policyID={3} onClose={noop} onUpserted={noop} />);

    expect(screen.getByLabelText(/identifiers/i)).toHaveValue("half a list");
    expect(screen.queryByRole("table")).toBeNull();
  });

  // spec:web-ui/unsaved-work-survives-a-reauthentication/saved-work-is-not-restored-when-it-is-stale-or-from-another-page
  it.each([
    ["a null payload", null],
    ["a phase it does not know", { phase: "done", rawInput: "", rows: [], enforcement: null, severity: "medium", reason: "" }],
    ["a row that is not one", { phase: "preview", rawInput: "", rows: [{ identifier: 1 }], enforcement: null, severity: "medium", reason: "" }],
    ["rows that are not a list", { phase: "preview", rawInput: "", rows: "nope", enforcement: null, severity: "medium", reason: "" }],
  ])("ignores %s under a valid envelope, and removes it", (_name, payload) => {
    saveDraft("appcontrol-paste-many:3", payload);

    render(<PasteManyModal open policyID={3} onClose={noop} onUpserted={noop} />);

    expect(screen.queryByText(RESTORED)).toBeNull();
    expect(screen.queryByRole("table")).toBeNull();
    expect(sessionStorage.getItem("rawtrail:reauth-draft:appcontrol-paste-many:3")).toBeNull();
  });
});

describe("PolicyDetail reopens the dialog whose work was saved", () => {
  function renderDetail() {
    return render(
      <PermissionsContext.Provider value={EVERY_RULE_ACTION}>
        <MemoryRouter initialEntries={["/app-control/policies/3"]}>
          <Routes>
            <Route path="/app-control/policies/:id" element={<PolicyDetail />} />
          </Routes>
        </MemoryRouter>
      </PermissionsContext.Provider>,
    );
  }

  beforeEach(() => {
    vi.spyOn(api, "getAppControlPolicy").mockResolvedValue(makePolicy());
  });

  // spec:web-ui/unsaved-work-survives-a-reauthentication/a-dialog-with-saved-work-is-reopened-by-its-page
  it("reopens Add rule with the work put back", async () => {
    const first = renderDetail();
    fireEvent.click(await screen.findByRole("button", { name: "Add rule" }));
    const typing = within(screen.getByRole("dialog", { name: "Add rule" }));
    fireEvent.change(typing.getByLabelText(/identifier/i), { target: { value: hex } });
    fireEvent.change(typing.getByLabelText(/reason/i), { target: { value: "block it" } });
    expect(persistReauthDrafts()).toBe(true);
    first.unmount();
    cleanup();

    renderDetail();

    const dialog = within(await screen.findByRole("dialog", { name: "Add rule" }));
    expect(dialog.getByText(RESTORED)).toBeVisible();
    expect(dialog.getByLabelText(/identifier/i)).toHaveValue(hex);
    expect(dialog.getByLabelText(/reason/i)).toHaveValue("block it");
  });

  // spec:web-ui/unsaved-work-survives-a-reauthentication/a-dialog-with-saved-work-is-reopened-by-its-page
  it("reopens Paste many with the work put back", async () => {
    const first = renderDetail();
    fireEvent.click(await screen.findByRole("button", { name: "Paste many" }));
    fireEvent.change(within(screen.getByRole("dialog", { name: "Paste many rules" })).getByLabelText(/identifiers/i), {
      target: { value: "EQHXZ8M8AV" },
    });
    persistReauthDrafts();
    first.unmount();

    renderDetail();

    const dialog = within(await screen.findByRole("dialog", { name: "Paste many rules" }));
    expect(dialog.getByText(RESTORED)).toBeVisible();
    expect(dialog.getByLabelText(/identifiers/i)).toHaveValue("EQHXZ8M8AV");
  });

  // spec:web-ui/unsaved-work-survives-a-reauthentication/a-dialog-with-saved-work-is-reopened-by-its-page
  it("reopens Edit on the rule whose work was saved, once the policy has loaded", async () => {
    const first = renderDetail();
    fireEvent.click(await screen.findByRole("button", { name: /^Actions for / }));
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.change(within(screen.getByRole("dialog", { name: "Edit rule" })).getByLabelText(/reason/i), { target: { value: "raise it" } });
    expect(persistReauthDrafts()).toBe(true);
    first.unmount();

    renderDetail();

    const dialog = within(await screen.findByRole("dialog", { name: "Edit rule" }));
    expect(dialog.getByText(RESTORED)).toBeVisible();
    expect(dialog.getByLabelText(/reason/i)).toHaveValue("raise it");
  });

  it("opens nothing when no work was saved", async () => {
    renderDetail();
    await screen.findByRole("button", { name: "Add rule" });

    await waitFor(() => {
      expect(screen.queryByText(RESTORED)).toBeNull();
    });
    expect(document.querySelectorAll("dialog[open]")).toHaveLength(0);
  });
});

// React replays mount effects in development, and the app runs in StrictMode there. A second run of the open effect that found nothing
// left to restore would reset the form the first run had just filled, so the restore has to hold across the replay and still give way
// to the reset on a real close and reopen.
describe("restoring under StrictMode", () => {
  // spec:web-ui/unsaved-work-survives-a-reauthentication/unsaved-work-is-restored-and-not-saved-for-the-operator
  it("puts the Add rule work back, and still resets on a real close and reopen", () => {
    const first = render(<AddRuleModal open policyID={3} onClose={noop} onCreated={noop} />);
    fireEvent.change(screen.getByLabelText(/identifier/i), { target: { value: hex } });
    persistReauthDrafts();
    first.unmount();

    const view = render(
      <StrictMode>
        <AddRuleModal open policyID={3} onClose={noop} onCreated={noop} />
      </StrictMode>,
    );
    expect(screen.getByText(RESTORED)).toBeVisible();
    expect(screen.getByLabelText(/identifier/i)).toHaveValue(hex);

    view.rerender(
      <StrictMode>
        <AddRuleModal open={false} policyID={3} onClose={noop} onCreated={noop} />
      </StrictMode>,
    );
    view.rerender(
      <StrictMode>
        <AddRuleModal open policyID={3} onClose={noop} onCreated={noop} />
      </StrictMode>,
    );
    expect(screen.getByLabelText(/identifier/i)).toHaveValue("");
    expect(screen.queryByText(RESTORED)).toBeNull();
  });

  it("puts the Edit rule work back", () => {
    const rule = makeRule();
    const first = render(<EditRuleModal open rule={rule} onClose={noop} onSaved={noop} />);
    fireEvent.change(screen.getByLabelText(/reason/i), { target: { value: "raise it" } });
    persistReauthDrafts();
    first.unmount();

    render(
      <StrictMode>
        <EditRuleModal open rule={rule} onClose={noop} onSaved={noop} />
      </StrictMode>,
    );

    expect(screen.getByText(RESTORED)).toBeVisible();
    expect(screen.getByLabelText(/reason/i)).toHaveValue("raise it");
  });

  it("puts the Paste many work back", () => {
    const first = render(<PasteManyModal open policyID={3} onClose={noop} onUpserted={noop} />);
    fireEvent.change(screen.getByLabelText(/identifiers/i), { target: { value: "half a list" } });
    persistReauthDrafts();
    first.unmount();

    render(
      <StrictMode>
        <PasteManyModal open policyID={3} onClose={noop} onUpserted={noop} />
      </StrictMode>,
    );

    expect(screen.getByText(RESTORED)).toBeVisible();
    expect(screen.getByLabelText(/identifiers/i)).toHaveValue("half a list");
  });

  it("reopens the page's dialog with the work put back", async () => {
    vi.spyOn(api, "getAppControlPolicy").mockResolvedValue(makePolicy());
    const mount = (strict: boolean) => {
      const tree = (
        <PermissionsContext.Provider value={EVERY_RULE_ACTION}>
          <MemoryRouter initialEntries={["/app-control/policies/3"]}>
            <Routes>
              <Route path="/app-control/policies/:id" element={<PolicyDetail />} />
            </Routes>
          </MemoryRouter>
        </PermissionsContext.Provider>
      );
      return render(strict ? <StrictMode>{tree}</StrictMode> : tree);
    };
    const first = mount(false);
    fireEvent.click(await screen.findByRole("button", { name: /^Actions for / }));
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.change(within(screen.getByRole("dialog", { name: "Edit rule" })).getByLabelText(/reason/i), { target: { value: "raise it" } });
    persistReauthDrafts();
    first.unmount();

    mount(true);

    const dialog = within(await screen.findByRole("dialog", { name: "Edit rule" }));
    expect(dialog.getByText(RESTORED)).toBeVisible();
    expect(dialog.getByLabelText(/reason/i)).toHaveValue("raise it");
  });
});
