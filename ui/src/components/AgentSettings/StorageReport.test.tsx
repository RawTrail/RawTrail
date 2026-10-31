import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { StorageReport } from "./StorageReport";
import * as api from "../../api";

const report = (over: Partial<api.StorageReport> = {}): api.StorageReport => ({
  event_archive_bytes: 80_000_000_000,
  database_bytes: 30_000_000_000,
  total_bytes: 110_000_000_000,
  event_retention_days: 7,
  warn_bytes: 200_000_000_000,
  over_warn: false,
  ...over,
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("StorageReport", () => {
  // spec:web-ui/data-collection-settings-show-the-store-size/the-page-shows-the-store-size-and-retention
  it("says how much disk the stores take and how long events are kept", async () => {
    vi.spyOn(api, "getStorageReport").mockResolvedValue(report());
    render(<StorageReport />);
    expect(
      await screen.findByText(
        "The server's stores take 110.0 GB: 80.0 GB in the event archive and 30.0 GB in the database, which holds processes, alerts, " +
          "settings and the events still waiting to be analyzed. Events are kept for 7 days.",
      ),
    ).toBeVisible();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  // spec:web-ui/data-collection-settings-show-the-store-size/the-page-warns-past-the-threshold
  it("warns, with what to do, when the stores are past the size the server warns at", async () => {
    vi.spyOn(api, "getStorageReport").mockResolvedValue(report({ warn_bytes: 100_000_000_000, over_warn: true, event_retention_days: 1 }));
    render(<StorageReport />);
    const warning = await screen.findByRole("alert");
    expect(warning).toBeVisible();
    expect(warning).toHaveTextContent("The stores are past the 100.0 GB this server warns at.");
    expect(warning).toHaveTextContent("EDR_EVENT_RETENTION_DAYS");
    expect(screen.getByText(/Events are kept for 1 day\./)).toBeVisible();
  });

  it("says the size could not be read, leaving the rest of the page to work", async () => {
    vi.spyOn(api, "getStorageReport").mockRejectedValue(new Error("clickhouse down"));
    render(<StorageReport />);
    expect(await screen.findByRole("alert")).toHaveTextContent("The storage size could not be read: clickhouse down");
  });
});
