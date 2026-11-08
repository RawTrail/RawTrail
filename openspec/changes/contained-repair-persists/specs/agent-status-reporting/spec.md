## MODIFIED Requirements

### Requirement: Remediation attempts are bounded and escalate on exhaustion

Repeated failure to restore a provider means the fault is not one that re-enabling fixes, so the agent SHALL bound how many times it retries and SHALL space successive attempts. The one exception is a contained host's content filter, whose repair goes on past the budget (see "A contained host's repair does not give up"). An unbounded repair loop would rewrite system configuration indefinitely and would hide the underlying fault behind apparently ongoing recovery.

When the attempt budget is exhausted the component SHALL report a reason distinct from the one it reports while remediation is still being attempted, so that an operator can tell "recovery is in progress" from "recovery failed and a human is required".

A successful remediation SHALL reset the budget, so a host that fails intermittently over a long period is retried each time rather than being permanently written off.

An enable that finishes after the stop it was started for has ended SHALL have its outcome discarded. A provider can be reported running, or disabled, while an enable is still running, and can stop again before that enable returns; attributing the finished attempt to the stop that now stands would spend a budget that stop never used, and on the last attempt of a budget would tell the operator recovery had been given up on a fault nothing had yet been tried for.

#### Scenario: Repeated failures stop retrying and escalate

- **GIVEN** a capture provider is reported stopped, on a host that is not contained or a provider other than the content filter
- **WHEN** every remediation attempt in the budget fails to restore it
- **THEN** the agent stops attempting further remediation for that stop
- **AND** the `network_extension` component reports that automatic recovery failed

#### Scenario: A successful remediation restores the budget

- **GIVEN** a provider was restored by remediation after earlier attempts failed
- **WHEN** the same provider is later reported stopped again
- **THEN** the agent attempts remediation again with a full budget

#### Scenario: An attempt that outlives its episode is discarded

- **GIVEN** an enable is in flight for a stopped provider
- **AND** that provider is reported running and then stops again before the enable returns
- **WHEN** the enable finishes
- **THEN** nothing of it is recorded against the stop that now stands
- **AND** no escalation is published for that stop, even when the finished attempt was the last of its own budget

## ADDED Requirements

### Requirement: A contained host's repair does not give up

While the host is network contained, its content filter is what isolates it. So spending the filter's remediation budget SHALL escalate as it does on any host, once per stop, and the agent SHALL then keep remediating it, with the wait between attempts capped at one minute, until it is restored or the host is released. Other providers, and a released host, SHALL keep the bounded budget.

#### Scenario: Repair goes on past the budget while contained

- **GIVEN** a contained host whose content filter stays stopped through every remediation in the budget
- **WHEN** the budget is spent
- **THEN** the component reports that automatic recovery failed, and the escalation is recorded once
- **AND** the agent keeps remediating, waiting no longer than one minute between attempts

#### Scenario: A released host's repair stops at the budget

- **GIVEN** a host whose repair went on past the budget while it was contained
- **WHEN** the host is released and the provider is still stopped
- **THEN** the agent stops remediating until an operator acts

#### Scenario: A contained host's DNS proxy keeps the bounded budget

- **GIVEN** a contained host whose DNS proxy stays stopped through every remediation in the budget
- **WHEN** the budget is spent
- **THEN** the agent stops remediating the DNS proxy, as on a host that is not contained
