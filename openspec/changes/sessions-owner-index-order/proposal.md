## Why

On the dogfood server running v0.9.0-rc.4, the AI agent sessions page shows an API 502 error (issue #1544). `GET /api/agent-sessions` runs past the server's 30 s write timeout. Each row reads its session's owner, the uid of the session's earliest process, twice: once for the owner and once for the bypass mark. In that correlated form MySQL 8.4 reads every one of the session's processes and sorts them before taking the first. Dogfood has a session with 762,084 processes, and a page of 51 sessions took 16.8 s per read.

## What changes

- The owner lookup orders by the whole `idx_processes_host_agent_session_fork` key (`host_id, agent_session_id, fork_time_ns, id`). Its first two columns are fixed by the lookup, so the owner it finds is the same, and MySQL now reads the session's processes in index order and stops at the first. On dogfood the same page's read drops from 16.8 s to 0.034 s.
- The sessions list's cost per page no longer grows with the number of processes its sessions hold.

## Impact

- `server/detection/internal/mysql/agent_sessions.go` and its plan test. The session summary and the bypass filter share the same lookup, so they are faster too.
