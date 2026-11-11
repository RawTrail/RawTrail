## Why

Every view of an AI agent session reads the session's processes whole, up to 20,000 of them: the Timeline, the Commands view, the processes search and the Summary. On dogfood (v0.9.0-rc.5) that read was most of each request's time, about a second for a long session, and the server repeated it on every page of the Timeline and every tab, and once per tab when the session view opened its tabs together (issue #1550).

## What changes

- The server keeps a session's process read for 30 seconds while the session's last activity stays the same, and serves the session's views from it. A session still running moves its last activity as its processes arrive, so its views keep reading it afresh.
- Views of one session that miss at the same time share one read.
- At most four reads are kept per server replica. The cache is per replica and safe to lose (ADR-0010): a replica without it reads the session again.

## Impact

- `server/detection/internal/operator`: `readSession` goes through the new cache.
- A view of an idle session can show the process read from up to 30 seconds earlier, for example a process's exit recorded in that time.
- Each kept read holds about 25 to 30 MB for a session at the 20,000-process bound, so the cache holds at most about 120 MB.
