## ADDED Requirements

### Requirement: A contained host that stops reporting raises an alert

The server SHALL raise one high-severity `contained_host_silent` alert for each containment of a host that has not checked in for more than 10 minutes, counting silence on the server's clock from the later of its last check-in and when it was contained, and SHALL not raise it again for that containment after the alert is pruned. A host still reporting SHALL NOT alert; one contained again SHALL alert again.

#### Scenario: A contained host silent past the threshold raises one alert

- **GIVEN** a contained host whose last check-in was 11 minutes ago, though its own clock stamped its last events a day ahead
- **WHEN** the check runs, and runs again, including after alert retention has deleted the first alert
- **THEN** one open, high-severity `contained_host_silent` alert names the host, and no second one is raised

#### Scenario: A contained host that still reports does not alert

- **GIVEN** a contained host that checked in a minute ago, or one contained 5 minutes ago that was already quiet
- **WHEN** the check runs
- **THEN** no alert is raised

#### Scenario: A new containment alerts again

- **GIVEN** a silent host that already alerted for one containment
- **WHEN** it is released and contained again, and stays silent
- **THEN** a second alert is raised for the new containment
