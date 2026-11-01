## ADDED Requirements

### Requirement: Agent session timeline view

The agent session view SHALL list what the session ran in one Timeline view, with a "Group by prompt" switch: on, it lists each prompt with the commands and tool calls made for it; off, it lists the commands alone in the order they started. It SHALL open grouped when the session recorded a prompt and ungrouped when it recorded none, keep the operator's choice in the browser across sessions, and open a link naming `conversation` or `commands` grouped or ungrouped whatever was chosen.

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

### Requirement: Timeline headers stay selectable

A Timeline turn or command SHALL open and close from a click anywhere on its header and from its own toggle button, and SHALL keep its header's text selectable: a click that selects text, or the second click of a double-click, SHALL leave it as it was. A prompt the operator may read, and a command, SHALL offer a button copying its text.

#### Scenario: Selecting a prompt does not close it

- **GIVEN** an open turn in the Timeline
- **WHEN** the operator drags across its prompt to select it, or double-clicks a word of it
- **THEN** the text is selected and the turn stays open
- **AND** its copy button copies the whole prompt without opening or closing it

## REMOVED Requirements

### Requirement: Stable session view switcher

**Reason**: It names the Commands view, which the Timeline replaces, and a MODIFIED block cannot drop the scenario that names it.

**Migration**: Replaced by "Stable session view header", which states the same rule for the Timeline, Processes and Activity views and covers both of the Timeline's switches.
