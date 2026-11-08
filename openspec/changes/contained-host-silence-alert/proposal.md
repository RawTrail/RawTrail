## Why

The v0.9.0-rc.3 check of #1228 found that a local administrator with root on a contained Mac can stop the agent's launchd job and then switch off the network filter. With the agent gone, nothing repairs the filter, and nothing reports the tamper. The server only sees the host go offline, the same grey pill as a closed laptop. A host an operator has isolated deserves a responder's attention when it goes quiet.

## What changes

- The server checks once a minute for contained hosts that have not checked in for more than 10 minutes. That is twice the 5 minutes after which the console calls a host offline, so a briefly closed lid does not alert. Silence counts from the later of the host's last check-in and the moment it was contained.
- Silence is read from `hosts.last_checkin_ns`, a new column only the server's own check-ins write (the command poll and the control stream), never `last_seen_ns`, which ingest advances from the timestamps the agent's clock puts on events. A host whose clock runs ahead cannot keep itself "seen".
- `hosts.silent_alerted_epoch` records the containment a host last alerted for, so alert retention deleting the first alert does not raise a second.
- Each such host gets one high-severity `contained_host_silent` alert per containment. The alert's dedup key names the containment, so the check runs on every replica without a lock, and a host released and contained again can alert again.
- The response context exposes the contained hosts. cmd/main hands them to detection, the same inversion as the undeliverable-command count.

## Impact

- `server/detection/internal/containedsilence` (new), detection migration `00026` and the store's `HostsCheckIn` and `MarkSilentAlerted`, `server/detection/bootstrap`, `server/response/bootstrap`, `server/cmd/rawtrail-server`.
- A new alert rule id, not a catalog rule: it comes from the server's view of a host rather than from events, so it ships no efficacy corpus and renders without a rule link, like `sensor_recovery_failed`.
