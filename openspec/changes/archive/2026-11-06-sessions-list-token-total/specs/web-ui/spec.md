## ADDED Requirements

### Requirement: Sessions list tokens

The Agent sessions page SHALL list each session's tokens compactly, with the exact count in the cell's title, and leave the cell empty when the server gives none.

#### Scenario: A session lists its tokens

- **GIVEN** a session that used 1,234,567 tokens
- **WHEN** the operator opens the Agent sessions page
- **THEN** its Tokens cell reads 1.2M, and its title gives the exact count
