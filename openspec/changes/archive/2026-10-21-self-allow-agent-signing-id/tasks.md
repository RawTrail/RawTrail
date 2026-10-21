## 1. Implementation

- [x] 1.1 Name the agent in the self-allow set by its signing identifier, and move the predicate where unit tests reach it.
- [x] 1.2 Unit test the failsafe against a TEAMID block on the release team, pin the set to the identifier the build signs the agent with, and mutation-check both.
- [ ] 1.3 Exercise on a SIP-on Mac (edr-qa) with a Developer ID release build: with a TEAMID block on the release team active, restart the agent and confirm it starts and connects. edr-dev cannot show this: with SIP off, Endpoint Security reports every exec as a platform binary with no team ID, so the platform carve-out allows it before any team rule is consulted. Needs the `io.rawtrail` provisioning profiles (`rename-to-rawtrail` task 2.5).
