## ADDED Requirements

### Requirement: Exec strings read by token length

The extension SHALL read an exec event's path and each argument by the length of its Endpoint Security string token and never past it. A token holding no bytes, or bytes that are not valid UTF-8, SHALL be reported as the empty string.

#### Scenario: A token is read to its length and no further

- **GIVEN** a string token whose bytes are not followed by a NUL
- **WHEN** the extension reads it
- **THEN** it reads exactly the token's length, keeping a NUL inside the token as part of it

#### Scenario: A token that is not UTF-8 reads empty

- **GIVEN** a string token whose bytes are not valid UTF-8
- **WHEN** the extension reads it
- **THEN** it reads as the empty string, not as text with replacement characters
