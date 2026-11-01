## ADDED Requirements

### Requirement: Agent session timeline view

The agent session view SHALL list what the session ran in one Timeline view, with a "Group by prompt" switch: on, it lists each prompt with the commands and tool calls made for it; off, it lists the commands alone in the order they started. It SHALL open grouped when the session recorded a prompt and ungrouped when it recorded none, keep the operator's choice across sessions, and open a link naming `conversation` or `commands` grouped or ungrouped.

#### Scenario: A session with prompts opens grouped by them

- **GIVEN** a session that recorded prompts
- **WHEN** the operator opens its Timeline
- **THEN** "Group by prompt" is on and each prompt is listed with what the agent did for it
- **AND** turning it off lists the session's commands alone

#### Scenario: A session with no prompt opens on its commands

- **GIVEN** a session that recorded no prompt
- **WHEN** the operator opens its Timeline
- **THEN** "Group by prompt" is off and the session's commands are listed

#### Scenario: A link to a replaced view opens the Timeline

- **GIVEN** a link to a session naming `view=conversation`
- **WHEN** the operator follows it
- **THEN** the session opens on its Timeline, grouped by prompt

### Requirement: Stable session view header

The agent session view SHALL lay out its header the same in the Timeline, Processes and Activity views, so the view switcher keeps its place: a header control one view does not use SHALL be hidden there while keeping its space, not removed.

#### Scenario: The Timeline's switches are hidden, not removed, outside it

- **GIVEN** a session view showing its Timeline, with the "Group by prompt" and "Show shell plumbing" switches
- **WHEN** the operator switches to Processes
- **THEN** both switches are hidden but keep their place in the header, and cannot be focused

## REMOVED Requirements

### Requirement: Stable session view switcher

**Reason**: It names the Commands view, which the Timeline replaces, and a MODIFIED block cannot drop the scenario that names it.

**Migration**: Replaced by "Stable session view header", which states the same rule for the Timeline, Processes and Activity views and covers both of the Timeline's switches.
