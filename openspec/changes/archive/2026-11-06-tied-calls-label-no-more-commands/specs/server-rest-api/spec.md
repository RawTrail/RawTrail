## ADDED Requirements

### Requirement: Tied calls label no more commands than they hold

The commands endpoint SHALL label commands with tool calls so that each labelled command can be given a call of its own: a matched command its call, and the commands of a tie distinct calls among the tie's. A command whose nearest calls cannot be given it so SHALL look further back, and SHALL go unlabelled when no earlier call can, or when its search reaches its bound of calls looked at.

#### Scenario: Overlapping cut calls label two commands at most

- **GIVEN** two calls of one turn cut to the agent's limit on a UTF-8 boundary, the shorter a prefix of the longer, and three commands: one matching both, then one matching only the shorter, then one matching both
- **WHEN** the session's commands are read
- **THEN** the first is ambiguous between the two calls, the second is matched to the shorter, and the third is unlabelled
- **AND** over random sessions of whole and cut calls with overlapping texts, every labelled command can be given a distinct call

#### Scenario: A search that reaches its bound leaves the command unlabelled

- **GIVEN** a tie its commands have used up, an older call of the same text, and a search bound too small to settle the next command
- **WHEN** the next command's search reaches the bound
- **THEN** the command is unlabelled rather than given the older call
