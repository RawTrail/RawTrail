## ADDED Requirements

### Requirement: A session's usage counts only from its owner

A session's token and permission-mode totals, its tokens in the sessions list, and its bypass mark SHALL count only `agent_usage` lines whose `uid` is the session's owner, the uid of its earliest retained process. A session whose owner is no longer known SHALL report no usage and no bypass mark.

#### Scenario: Another user's transcript changes nothing

- **GIVEN** a session owned by uid 501, and a line in the bypass mode with thousands of tokens that names it from uid 502's transcript
- **WHEN** the session's summary and the sessions list are read
- **THEN** the session's tokens are its owner's alone and it is not marked or filtered as bypassed
- **AND** a session with no retained process is neither marked nor given tokens by any line
