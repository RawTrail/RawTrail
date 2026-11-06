## 1. Full Disk Access reporting

- [x] 1.1 The extension records an ES client refusal and clears it on success
- [x] 1.2 The agent reports the extension's refusal and its own grant
- [x] 1.3 Live QA on edr-dev (SIP off): the build deploys, the extension clears a planted refusal once its client starts, the agent reports its own grant, and a stale refusal leaves a connected extension healthy
- [ ] 1.4 Before the release candidate, on a Mac with SIP on and the signed build: revoke Full Disk Access from each and see `full_disk_access_missing`, then restore it. SIP off disables Full Disk Access enforcement, so edr-dev cannot refuse the client, and edr-qa runs only signed builds
