## REMOVED Requirements

### Requirement: Host event timeline view

**Reason**: Its alert-chain scope was assembled by the client from the graph's tree read, so most of its text described that read's states (in flight, failed, truncated). The server now resolves the chain.

**Migration**: Restated as "Host timeline view" below, under 500 characters, with the scope resolved by the server.

## ADDED Requirements

### Requirement: Host timeline view

The host page SHALL offer a timeline view beside the process graph, selected in the URL, listing the active window's exec, connection and DNS events newest first with their process and detail, filterable by type and text, paged on demand, with the total. With the alert-chain focus on, it SHALL scope to the chain the server resolves for the alerted process and state how far that scope reached, and with it off show the whole host.

#### Scenario: Timeline view lists window events filterable by type

- **GIVEN** a host with exec, network, and DNS events in the active window
- **WHEN** the operator switches to the timeline view
- **THEN** the events are listed newest-first with their type and originating process
- **AND** selecting an event-type filter narrows the list to that type

#### Scenario: Timeline scopes to the alert chain

- **GIVEN** the host page entered for an alert with the alert-chain focus active
- **WHEN** the operator switches to the timeline view
- **THEN** the timeline asks the server for the alerted process's chain, lists only its events and indicates it is scoped to the chain
- **AND** turning the shared focus off widens the timeline to the full host event stream

#### Scenario: Timeline says so when it cannot scope to the chain

- **GIVEN** the host page entered for an alert whose chain processes carry no process generation
- **WHEN** the operator switches to the timeline view
- **THEN** the timeline lists the full host event stream
- **AND** it states that it is showing the whole host because the chain cannot be narrowed
- **AND** it does not simultaneously claim to be scoped to the alert chain

#### Scenario: Timeline distinguishes an absent chain from a chain without generations

- **GIVEN** the host page entered for an alert whose alerted process is no longer stored
- **WHEN** the operator switches to the timeline view
- **THEN** the timeline lists the full host event stream
- **AND** it states that the process is no longer stored
- **AND** it does not attribute the fallback to missing generation data

#### Scenario: Timeline admits an alert chain it could only partly scope

- **GIVEN** the host page entered for an alert whose chain carries a process generation on some of its processes but not all, or has more processes than the timeline can scope to
- **WHEN** the operator switches to the timeline view
- **THEN** the timeline scopes to the processes it can reach
- **AND** it states that only part of the chain was reached, and why
- **AND** it does not present itself as scoped to the whole alert chain

#### Scenario: The graph and timeline share one time window

- **GIVEN** the host page with a time window set on the graph view
- **WHEN** the operator switches to the timeline view
- **THEN** the timeline queries the same window
- **AND** switching back to the graph leaves the window unchanged

#### Scenario: A text filter narrows the timeline

- **GIVEN** the timeline view is displayed
- **WHEN** the operator enters a text filter
- **THEN** only events whose payload matches the text are listed
