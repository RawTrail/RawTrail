## Why

When the server stops while the processor is working on a claimed batch, the batch is not handed back. Shutdown cancels the processor's context, the batch fails, and the statement that returns it to the queue runs on that same canceled context, so it never reaches MySQL. The rows stay claimed with no owner, and since a claim stops at its host's oldest in-flight event, every later event for that host waits out the 5-minute claim lease. After a deploy or restart, the affected hosts' alerts and timeline entries arrive up to 5 minutes late (#1571, seen on dogfood with v0.9.0-rc.8).

## What changes

- The processor settles the batch it holds, whether by handing it back or by acknowledging it, on a context detached from the stop, bounded by a short grace once the stop arrives. This covers the requeue after a failed fold, the requeue after a failed evaluation, and the acknowledgement of a batch that finished evaluating as the stop arrived.
- The claim lease stays the fallback for a crash, where nothing can run.

## Impact

- `server/detection/internal/pipeline/processor.go`.
- A graceful stop now counts one attempt against the batch it hands back, as any other requeue does. A batch needs both 20 attempts and 15 minutes of failure before it is set aside, so a stop cannot set one aside on its own.
