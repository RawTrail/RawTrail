## ADDED Requirements

### Requirement: DNS activity lists a name once

An activity read's DNS list SHALL hold one row a name over every process of the scope that resolved it: the name's count, first and last over all of them, the union of their answers up to its bound, the first 16 processes by when each first resolved it, each with that time, and how many processes resolved it. A page SHALL continue the list by name.

#### Scenario: A name several processes resolved is one row

- **GIVEN** seventeen processes that resolved one name, one of them twice and one with another answer
- **WHEN** the scope's activity is read
- **THEN** the DNS list holds the name once, counting every lookup, from the first to the last, with both answers
- **AND** it lists the sixteen processes that resolved it first, earliest first, and says seventeen did
