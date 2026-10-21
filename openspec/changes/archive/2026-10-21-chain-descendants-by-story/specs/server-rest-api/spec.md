## ADDED Requirements

### Requirement: Alert chain descendants by story

When the pinned process carries a story root, the chain read SHALL fetch its candidate descendants in one indexed query, the story's processes and unstamped ones forked from the pinned process's fork to the window's end, and select descendants by the walk's own parent and lifetime rule, without the walk's descendant cap. A process without a story root, or a story past the candidate bound, SHALL be walked as before.

#### Scenario: The story read matches the walk

- **GIVEN** a pinned process whose descendants include a child forked before its creator's row was recorded, beside an unrelated process on the host
- **WHEN** its chain is read with and without a story root
- **THEN** both reads return the same descendants under the same parents
- **AND** the unstamped child is included and the unrelated process is not

#### Scenario: A chain past the walk's cap is read whole

- **GIVEN** a pinned process with more descendants than the walk's cap, in its story
- **WHEN** its chain is read
- **THEN** every descendant is returned and the read is not truncated
- **AND** the descendants cost one query whatever their depth
