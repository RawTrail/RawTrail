## ADDED Requirements

### Requirement: The session view searches its processes and activity on the server

The agent session view SHALL send a search in Processes to the server and list each process it found alone, with the rest on request, rather than searching only the pages it has loaded. In Activity it SHALL read each class's rows for the search from the server, keeping the session's totals.

#### Scenario: A Processes search finds a process not yet loaded

- **GIVEN** a session whose matching process lies past the tree pages loaded
- **WHEN** the operator searches in Processes
- **THEN** the view lists that process from the server's search, with more on request

#### Scenario: An Activity search reads the class from the server

- **GIVEN** a session's Activity
- **WHEN** the operator searches
- **THEN** each class lists the rows the server's search for it returns
