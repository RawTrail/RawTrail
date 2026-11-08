## ADDED Requirements

### Requirement: A contained host's repair does not give up

While the host is network contained, its content filter is what isolates it. So spending the remediation budget SHALL escalate as it does on any host, once per stop, and the agent SHALL then keep remediating, with the wait between attempts capped at one minute, until the provider is restored or the host is released. A released host SHALL return to the bounded budget.

#### Scenario: Repair goes on past the budget while contained

- **GIVEN** a contained host whose content filter stays stopped through every remediation in the budget
- **WHEN** the budget is spent
- **THEN** the component reports that automatic recovery failed, and the escalation is recorded once
- **AND** the agent keeps remediating, waiting no longer than one minute between attempts

#### Scenario: A released host's repair stops at the budget

- **GIVEN** a host whose repair went on past the budget while it was contained
- **WHEN** the host is released and the provider is still stopped
- **THEN** the agent stops remediating until an operator acts
