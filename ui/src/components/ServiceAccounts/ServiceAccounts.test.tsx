import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import * as api from "../../api";
import { ServiceAccounts } from "./ServiceAccounts";

const baseAccounts: api.ServiceAccount[] = [
  {
    id: 1, client_id: "sa_abc", name: "ci-pipeline", role: "analyst", status: "active",
    created_at: "2026-06-01T00:00:00Z", expires_at: "2026-09-01T00:00:00Z",
  },
  {
    id: 2, client_id: "sa_def", name: "old-bot", role: "auditor", status: "revoked",
    created_at: "2026-05-01T00:00:00Z", expires_at: "2026-08-01T00:00:00Z", last_used_at: "2026-06-10T00:00:00Z",
  },
];

// inDays is an RFC 3339 instant that many days from now, plus an hour so that "in N days" is not a boundary case.
function inDays(days: number): string {
  return new Date(Date.now() + days * 24 * 3_600_000 + 3_600_000).toISOString();
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ServiceAccounts", () => {
  // spec:server-identity-service-accounts/service-accounts-are-managed-from-an-admin-surface-behind-the-chokepoint/page-is-hidden-without-the-grant
  it("renders the list with role and status badges", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue(baseAccounts);
    render(<ServiceAccounts />);
    expect(await screen.findByText("ci-pipeline")).toBeInTheDocument();
    expect(screen.getByText("sa_abc")).toBeInTheDocument();
    expect(screen.getByText("active")).toBeInTheDocument();
    expect(screen.getByText("revoked")).toBeInTheDocument();
    expect(screen.getByText("Analyst")).toBeInTheDocument();
    expect(screen.getByText("Auditor")).toBeInTheDocument();
    // Account 1 was never used.
    expect(screen.getByText("Never")).toBeInTheDocument();
  });

  it("shows an empty state", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue([]);
    render(<ServiceAccounts />);
    expect(await screen.findByText("No service accounts yet.")).toBeInTheDocument();
  });

  it("renders the load error state", async () => {
    vi.spyOn(api, "listServiceAccounts").mockRejectedValue(new Error("boom"));
    render(<ServiceAccounts />);
    expect(await screen.findByText(/Error: boom/)).toBeInTheDocument();
  });

  it("creates a service account and shows the one-time secret", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue([]);
    const create = vi.spyOn(api, "createServiceAccount").mockResolvedValue({
      id: 3, client_id: "sa_new", name: "bot", role: "analyst", status: "active",
      created_at: "2026-06-20T00:00:00Z", expires_at: "2026-09-18T00:00:00Z", secret: "edrsa_one_time",
    });
    render(<ServiceAccounts />);
    await screen.findByText("No service accounts yet.");

    fireEvent.click(screen.getByRole("button", { name: "Create service account" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "bot" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => { expect(create).toHaveBeenCalledTimes(1); });
    expect(create.mock.calls[0][0]).toEqual({ name: "bot", role: "analyst" });
    expect(await screen.findByLabelText("Client secret")).toHaveValue("edrsa_one_time");
    expect(screen.getByLabelText("Client ID")).toHaveValue("sa_new");
  });

  it("passes expires_in_days when provided", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue([]);
    const create = vi.spyOn(api, "createServiceAccount").mockResolvedValue({
      id: 4, client_id: "sa_x", name: "x", role: "auditor", status: "active",
      created_at: "2026-06-20T00:00:00Z", expires_at: "2026-07-20T00:00:00Z", secret: "edrsa_x",
    });
    render(<ServiceAccounts />);
    await screen.findByText("No service accounts yet.");

    fireEvent.click(screen.getByRole("button", { name: "Create service account" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "x" } });
    fireEvent.change(screen.getByLabelText("Role"), { target: { value: "auditor" } });
    fireEvent.change(screen.getByLabelText("Expires in (days)"), { target: { value: "30" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => { expect(create).toHaveBeenCalledTimes(1); });
    expect(create.mock.calls[0][0]).toEqual({ name: "x", role: "auditor", expires_in_days: 30 });
  });

  it("creates an admin service account (admin is in the allowlist)", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue([]);
    const create = vi.spyOn(api, "createServiceAccount").mockResolvedValue({
      id: 9, client_id: "sa_adm", name: "iac", role: "admin", status: "active",
      created_at: "2026-06-21T00:00:00Z", expires_at: "2026-09-19T00:00:00Z", secret: "edrsa_adm",
    });
    render(<ServiceAccounts />);
    await screen.findByText("No service accounts yet.");

    fireEvent.click(screen.getByRole("button", { name: "Create service account" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "iac" } });
    fireEvent.change(screen.getByLabelText("Role"), { target: { value: "admin" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => { expect(create).toHaveBeenCalledTimes(1); });
    expect(create.mock.calls[0][0]).toEqual({ name: "iac", role: "admin" });
  });

  it("rejects an invalid expiry without calling the API", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue([]);
    const create = vi.spyOn(api, "createServiceAccount");
    render(<ServiceAccounts />);
    await screen.findByText("No service accounts yet.");

    fireEvent.click(screen.getByRole("button", { name: "Create service account" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "bot" } });
    fireEvent.change(screen.getByLabelText("Expires in (days)"), { target: { value: "12.5" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/whole number from 1 to 365/);
    expect(create).not.toHaveBeenCalled();
  });

  it("requires a name", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue([]);
    const create = vi.spyOn(api, "createServiceAccount");
    render(<ServiceAccounts />);
    await screen.findByText("No service accounts yet.");

    fireEvent.click(screen.getByRole("button", { name: "Create service account" }));
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Name is required.");
    expect(create).not.toHaveBeenCalled();
  });

  it("surfaces a create error", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue([]);
    vi.spyOn(api, "createServiceAccount").mockRejectedValue(new Error("API error: 400 Bad Request"));
    render(<ServiceAccounts />);
    await screen.findByText("No service accounts yet.");

    fireEvent.click(screen.getByRole("button", { name: "Create service account" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "bot" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("API error: 400");
  });

  it("rotates a secret and shows the new value", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue(baseAccounts);
    const rotate = vi.spyOn(api, "rotateServiceAccount").mockResolvedValue({
      ...baseAccounts[0], expires_at: inDays(90), secret: "edrsa_rotated",
    });
    render(<ServiceAccounts />);
    await screen.findByText("ci-pipeline");

    fireEvent.click(screen.getByRole("button", { name: "Rotate" }));
    await waitFor(() => { expect(rotate).toHaveBeenCalledWith(1); });
    expect(await screen.findByLabelText("Client secret")).toHaveValue("edrsa_rotated");
  });

  // spec:web-ui/the-service-accounts-page-shows-when-each-credential-expires/the-secret-card-states-the-expiry
  it("states the new expiry on the secret card after a rotate and after a create", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue(baseAccounts);
    vi.spyOn(api, "rotateServiceAccount").mockResolvedValue({ ...baseAccounts[0], expires_at: inDays(90), secret: "edrsa_rotated" });
    vi.spyOn(api, "createServiceAccount").mockResolvedValue({
      id: 3, client_id: "sa_new", name: "bot", role: "analyst", status: "active",
      created_at: "2026-06-20T00:00:00Z", expires_at: inDays(30), secret: "edrsa_new",
    });
    render(<ServiceAccounts />);
    await screen.findByText("ci-pipeline");

    fireEvent.click(screen.getByRole("button", { name: "Rotate" }));
    const card = (await screen.findByText("Save this secret now")).closest('[role="status"]');
    expect(card).toHaveTextContent(`expires on ${new Date(inDays(90)).toLocaleDateString()}`);
    expect(card).toHaveTextContent("in 90 days");
    fireEvent.click(screen.getByRole("button", { name: "Done" }));

    fireEvent.click(screen.getByRole("button", { name: "Create service account" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "bot" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    const created = (await screen.findByText("Save this secret now")).closest('[role="status"]');
    expect(created).toHaveTextContent(`expires on ${new Date(inDays(30)).toLocaleDateString()}`);
    expect(created).toHaveTextContent("in 30 days");
  });

  // spec:web-ui/the-service-accounts-page-shows-when-each-credential-expires/the-list-shows-expiry-and-flags-a-near-one
  it("shows each account's expiry with the time left, and flags only an active one within 14 days", async () => {
    const account = (over: Partial<api.ServiceAccount>): api.ServiceAccount => ({
      id: 10, client_id: "sa_x", name: "x", role: "analyst", status: "active", created_at: "2026-06-01T00:00:00Z",
      expires_at: inDays(60), ...over,
    });
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue([
      account({ id: 11, name: "soon-bot", client_id: "sa_soon", expires_at: inDays(5) }),
      account({ id: 12, name: "later-bot", client_id: "sa_later", expires_at: inDays(60) }),
      account({ id: 13, name: "lapsed-bot", client_id: "sa_lapsed", status: "expired", expires_at: inDays(-2) }),
      account({ id: 14, name: "dead-bot", client_id: "sa_dead", status: "revoked", expires_at: inDays(5) }),
    ]);
    render(<ServiceAccounts />);
    await screen.findByText("soon-bot");

    expect(screen.getByRole("columnheader", { name: "Expires" })).toBeVisible();
    const row = (name: string) => screen.getByText(name).closest("tr") as HTMLElement;
    expect(row("soon-bot")).toHaveTextContent("in 5 days");
    expect(row("soon-bot")).toHaveTextContent("expires soon");
    expect(row("later-bot")).toHaveTextContent("in 60 days");
    expect(row("later-bot")).not.toHaveTextContent("expires soon");
    expect(row("lapsed-bot")).toHaveTextContent("expired");
    expect(row("lapsed-bot")).not.toHaveTextContent("expires soon");
    // A revoked account has stopped already; its status badge says so, and a near expiry date adds nothing to it.
    expect(row("dead-bot")).not.toHaveTextContent("expires soon");
  });

  // spec:web-ui/the-service-accounts-page-shows-when-each-credential-expires/a-dead-account-offers-no-rotate
  it("offers Rotate only on an active account", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue([
      { id: 21, client_id: "sa_live", name: "live-bot", role: "analyst", status: "active", created_at: "2026-06-01T00:00:00Z", expires_at: inDays(40) },
      { id: 22, client_id: "sa_lapsed", name: "lapsed-bot", role: "analyst", status: "expired", created_at: "2026-06-01T00:00:00Z", expires_at: inDays(-1) },
    ]);
    render(<ServiceAccounts />);
    await screen.findByText("live-bot");

    const rotates = screen.getAllByRole("button", { name: "Rotate" });
    expect(rotates).toHaveLength(1);
    expect(rotates[0].closest("tr")).toHaveTextContent("live-bot");
    // An expired account can still be revoked.
    expect(within(screen.getByText("lapsed-bot").closest("tr") as HTMLElement).getByRole("button", { name: "Revoke" })).toBeVisible();
  });

  it("surfaces a rotate error", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue(baseAccounts);
    vi.spyOn(api, "rotateServiceAccount").mockRejectedValue(new Error("rotate failed"));
    render(<ServiceAccounts />);
    await screen.findByText("ci-pipeline");
    fireEvent.click(screen.getByRole("button", { name: "Rotate" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("rotate failed");
  });

  it("revokes only after confirmation", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue(baseAccounts);
    const revoke = vi.spyOn(api, "revokeServiceAccount").mockResolvedValue();
    render(<ServiceAccounts />);
    await screen.findByText("ci-pipeline");

    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    expect(revoke).not.toHaveBeenCalled();
    expect(screen.getByText(/Revoke ci-pipeline\?/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await waitFor(() => { expect(revoke).toHaveBeenCalledWith(1); });
  });

  it("cancels a revoke confirmation without calling the API", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue(baseAccounts);
    const revoke = vi.spyOn(api, "revokeServiceAccount").mockResolvedValue();
    render(<ServiceAccounts />);
    await screen.findByText("ci-pipeline");

    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(revoke).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Revoke" })).toBeInTheDocument();
  });

  it("offers no rotate/revoke actions on a revoked account", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue([baseAccounts[1]]); // revoked only
    render(<ServiceAccounts />);
    await screen.findByText("old-bot");
    expect(screen.queryByRole("button", { name: "Rotate" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Revoke" })).not.toBeInTheDocument();
  });

  it("copies the issued secret when the clipboard API is available", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue([]);
    vi.spyOn(api, "createServiceAccount").mockResolvedValue({
      id: 5, client_id: "sa_c", name: "c", role: "analyst", status: "active",
      created_at: "2026-06-20T00:00:00Z", expires_at: "2026-09-18T00:00:00Z", secret: "edrsa_copy",
    });
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    render(<ServiceAccounts />);
    await screen.findByText("No service accounts yet.");

    fireEvent.click(screen.getByRole("button", { name: "Create service account" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "c" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    await screen.findByLabelText("Client secret");
    fireEvent.click(screen.getByRole("button", { name: "Copy client secret" }));
    await waitFor(() => { expect(writeText).toHaveBeenCalledWith("edrsa_copy"); });
    vi.unstubAllGlobals();

    // Dismissing the secret callout hides it.
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByLabelText("Client secret")).not.toBeInTheDocument();
  });

  it("does not throw copying without a clipboard API", async () => {
    vi.spyOn(api, "listServiceAccounts").mockResolvedValue([]);
    vi.spyOn(api, "createServiceAccount").mockResolvedValue({
      id: 6, client_id: "sa_d", name: "d", role: "analyst", status: "active",
      created_at: "2026-06-20T00:00:00Z", expires_at: "2026-09-18T00:00:00Z", secret: "edrsa_d",
    });
    vi.stubGlobal("navigator", {});
    render(<ServiceAccounts />);
    await screen.findByText("No service accounts yet.");
    fireEvent.click(screen.getByRole("button", { name: "Create service account" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "d" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    await screen.findByLabelText("Client secret");
    expect(() => { fireEvent.click(screen.getByRole("button", { name: "Copy client secret" })); }).not.toThrow();
    vi.unstubAllGlobals();
  });
});
