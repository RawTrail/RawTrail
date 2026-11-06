## ADDED Requirements

### Requirement: Queue reads cost the same at any backlog depth

Choosing the next host to process and claiming a batch for it SHALL each read work bounded by the number of hosts with queued events and the batch size, not by the number of events queued, so a backlog drains at the rate batches are claimed and processed however deep it is.

#### Scenario: A deep backlog does not slow the next host or the next claim

- **GIVEN** one host with tens of thousands of never-claimed events queued and other hosts with a few each
- **WHEN** the processor chooses the next host and claims a batch of 500 from the deep host
- **THEN** choosing the host reads one index entry per host for the never-claimed events, not every queued event
- **AND** each of the claim's reads walks that host's events in time order and stops at the batch size, without sorting its backlog

#### Scenario: A claim takes the oldest events across both claim states

- **GIVEN** a host whose queued events interleave in time between never-claimed events and events whose earlier claim expired
- **WHEN** a batch smaller than the host's queue is claimed
- **THEN** the batch holds the host's oldest events of both states together, in event order
