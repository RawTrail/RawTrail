## 1. Processor

- [x] 1.1 Settle a claimed batch on a context detached from the stop, bounded once the stop arrives, on both requeue paths and the acknowledgement.

## 2. Tests

- [x] 2.1 Integration tests that stop a processor during the fold, during evaluation, and after evaluation, and assert the batch is not left claimed.
- [x] 2.2 Integration test that a restarted processor drains the host's backlog without waiting for the claim lease.

## 3. Verify

- [x] 3.1 Stop the dev server while it drains a backlog and confirm no events stay claimed and the backlog resumes on restart.
