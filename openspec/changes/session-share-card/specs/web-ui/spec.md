## ADDED Requirements

### Requirement: A shared session image carries no identifying detail

A session shared as an image SHALL be drawn only from a card of counts, program names without arguments and domains, every string on which has usernames, hostnames, IP addresses and user@host pairs removed and home folders shown as `~`. An address SHALL never appear as a domain, and the title, project name and internal domains SHALL appear only when the person chooses them.

#### Scenario: What names a person or a machine is removed

- **GIVEN** text holding a home folder, a user@host pair, IPv4 and IPv6 addresses, an internal hostname, and the session's own username and hostname
- **WHEN** it is redacted
- **THEN** the home folder reads `~` and each of the others is removed, while a longer word containing the username is left alone

#### Scenario: The card shows only what the person chose

- **GIVEN** a session in a project, with programs run by full path, public, internal and address domains, and a read the credential guard stopped
- **WHEN** its card is built with the default choices
- **THEN** it shows no title and no project, programs by name only, public domains only, and the stopped read in the sharer's words

#### Scenario: No username, hostname or address survives

- **GIVEN** any session whose agent name, project, programs, domains and chosen title carry its username, hostname and IP addresses
- **WHEN** its card, caption and alt text are built with any choices
- **THEN** none of them contains the username, the hostname, its first label or any of the addresses
