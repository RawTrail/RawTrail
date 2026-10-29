// FanoutCounts is how far a pushed change reached the enrolled hosts.
export interface FanoutCounts {
  readonly fanout_hosts: number;
  readonly fanout_failed: number;
  readonly fanout_skipped_reason?: string;
}

// PushNotice says how far a saved change's push to hosts reached. A push skipped because the host list could not be read is called
// out, since "sent to 0 hosts" would read as a deployment with no hosts. noun names what was pushed, such as "set".
export function PushNotice({ version, counts, noun }: { readonly version: number; readonly counts: FanoutCounts; readonly noun: string }) {
  if (counts.fanout_skipped_reason !== undefined) {
    return (
      <div className="detection-config__error" role="alert">
        Saved as version {version}, but not sent: the enrolled hosts could not be listed. Hosts receive the {noun} within minutes once they
        can be.
      </div>
    );
  }
  const queued = counts.fanout_hosts - counts.fanout_failed;
  return (
    <p className="detection-config__note" role="status">
      Saved as version {version}. Queued for {queued} of {counts.fanout_hosts} enrolled{" "}
      {counts.fanout_hosts === 1 ? "host" : "hosts"}
      {counts.fanout_failed > 0 ? "; the rest receive it within minutes." : "."}
    </p>
  );
}
