## ADDED Requirements

### Requirement: A contained host that stops reporting raises an alert

The server SHALL raise one high-severity `contained_host_silent` alert for each containment of a host that has not checked in for more than 10 minutes, counting silence from the later of its last check-in and when it was contained. A host still reporting SHALL NOT alert, and a host released and contained again SHALL alert again.

#### Scenario: A contained host silent past the threshold raises one alert

- **GIVEN** a contained host whose last check-in was 11 minutes ago
- **WHEN** the check runs, and runs again
- **THEN** one open, high-severity `contained_host_silent` alert names the host, and no second one is raised

#### Scenario: A contained host that still reports does not alert

- **GIVEN** a contained host that checked in a minute ago, or one contained 5 minutes ago that was already quiet
- **WHEN** the check runs
- **THEN** no alert is raised

#### Scenario: A new containment alerts again

- **GIVEN** a silent host that already alerted for one containment
- **WHEN** it is released and contained again, and stays silent
- **THEN** a second alert is raised for the new containment
