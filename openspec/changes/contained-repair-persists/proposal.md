## Why

On v0.9.0-rc.3, issue #1228 found that a local admin with root can switch off a contained host's content filter. The agent's self-heal restores it about 30 s later. But the self-heal gives up after three failed repairs within one stop. So an admin who keeps switching the filter off before the extension reports it running could leave a contained host reachable, with only the alerts to show for it. The filter is what isolates a contained host, so for a contained host, giving up is the wrong trade.

## What changes

- While the host is network contained, spending the self-heal budget still escalates once, with the same `self_heal_failed` health reason and the same recovery-failed event. The agent then keeps repairing, with the wait between attempts capped at one minute.
- A host that is released returns to the normal bound: once the budget is spent, it waits for an operator.
- `containment.Manager.Contained()` exposes the extension-confirmed state to the self-heal.

## Impact

- `agent/selfheal/controller.go`, `agent/containment/containment.go`, `agent/cmd/rawtrail-agent/sensors_notwindows.go`, and their tests.
