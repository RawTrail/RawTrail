## ADDED Requirements

### Requirement: Activity reads load in parts

The activity rollup SHALL read its totals alone when asked with `rows=false`, leaving its lists empty. The rollup and each activity page SHALL name only the scope's processes their rows were matched by, a DNS row by each process that resolved its name. A session's activity reads SHALL read its processes through the read its other views keep.

#### Scenario: A response names the processes of its rows

- **GIVEN** a scope of several processes whose activity rows name one of them
- **WHEN** its rollup or a page of one of its classes is read
- **THEN** the response names that process alone

#### Scenario: The totals are read alone

- **GIVEN** a scope with files, connections and DNS lookups
- **WHEN** its rollup is read with `rows=false`
- **THEN** it holds the same totals as a full rollup and no rows

#### Scenario: A session's activity reads share its process read

- **GIVEN** an idle AI agent session
- **WHEN** its activity totals, each class's first page and its processes are read
- **THEN** the session's processes are read once
