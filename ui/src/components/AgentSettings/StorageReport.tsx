import { useEffect, useState } from "react";
import { getStorageReport, type StorageReport as Report } from "../../api";
import { formatBytes } from "./formatBytes";

// StorageReport says how much disk the server's stores take and how long events are kept (issue #1434), and warns when they are past
// the size set by EDR_STORE_SIZE_WARN_GB. Its read failing says so here and leaves the rest of the page alone.
export function StorageReport() {
  const [report, setReport] = useState<Report | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    getStorageReport()
      .then((r) => {
        if (!cancelled) setReport(r);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <>
      <h2 className="agent-settings__title agent-settings__title--section">Storage</h2>
      {error !== null && <p className="agent-settings__error" role="alert">The storage size could not be read: {error}</p>}
      {error === null && report === null && <p className="agent-settings__note">Loading...</p>}
      {report !== null && (
        <>
          <p className="agent-settings__statement">
            The server&apos;s stores take {formatBytes(report.total_bytes)}: {formatBytes(report.event_archive_bytes)} in the event
            archive and {formatBytes(report.database_bytes)} in the database, which holds processes, alerts, settings and the events
            still waiting to be analyzed. Events are kept for{" "}
            {report.event_retention_days === 1 ? "1 day" : `${String(report.event_retention_days)} days`}.
          </p>
          {report.over_warn && (
            <p className="agent-settings__error" role="alert">
              The stores are past the {formatBytes(report.warn_bytes)} this server warns at. Keep events for fewer days with{" "}
              <code>EDR_EVENT_RETENTION_DAYS</code>, or move the server to a larger disk.
            </p>
          )}
          <p className="agent-settings__note">
            Set how long events are kept with <code>EDR_EVENT_RETENTION_DAYS</code> and the size this page warns at with{" "}
            <code>EDR_STORE_SIZE_WARN_GB</code> (0 never warns). Sizes are what the stores report, compressed for events.
          </p>
        </>
      )}
    </>
  );
}
