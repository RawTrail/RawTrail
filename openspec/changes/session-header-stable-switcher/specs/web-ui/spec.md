## ADDED Requirements

### Requirement: Stable session view switcher

The agent session view SHALL lay out its header the same in the Commands, Processes and Activity views, so the view switcher keeps its place: a header control one view does not use SHALL be hidden there while keeping its space, not removed.

#### Scenario: The plumbing toggle is hidden, not removed, outside Commands

- **GIVEN** a session view showing Commands, with the "Show shell plumbing" toggle
- **WHEN** the operator switches to Processes
- **THEN** the toggle is hidden but keeps its place in the header, and cannot be focused
