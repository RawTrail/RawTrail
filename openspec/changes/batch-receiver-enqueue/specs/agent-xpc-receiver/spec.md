## ADDED Requirements

### Requirement: A burst is written in batches

The agent SHALL write the events a receiver delivers to its queue in batches: one transaction for the events waiting together in the receiver's channel, up to a bound, written once the channel is empty. A batch SHALL be durable when its write returns and SHALL fail whole on a storage error. Network and DNS events keep the coalescer's own flush. What was gathered SHALL be written when the connection ends or the agent stops, before the queue closes.

#### Scenario: A burst of events is written in batches

- **GIVEN** a burst of process activity that leaves more events waiting in the receiver's channel than the bound
- **WHEN** the agent takes them
- **THEN** it writes them in batches of at most the bound, the last as soon as the channel is empty, each in one transaction and in the order delivered, while network and DNS events among them are buffered and merged by the coalescer and written at its flush, as before
- **AND** on edr-dev a storm of 288,000 events in 24 seconds was written with no event dropped, where per-event commits had dropped about 120,000 of 144,000 in 11 seconds
