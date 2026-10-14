## ADDED Requirements

### Requirement: Tool calls beside their processes

The session view SHALL pair each group an agent's wrapper shell started with the Bash call that ran its command, taking the nearest unclaimed call made before the group started, and SHALL show that call's description and `tool_use_id`. Groups whose candidate calls share one time SHALL be marked as one of several identical calls, naming them, and not paired.

#### Scenario: Each Bash group shows its tool call

- **GIVEN** a session whose transcript recorded a Bash call with a description
- **WHEN** the session view lists the group its shell started
- **THEN** the group shows the call's description and `tool_use_id`

#### Scenario: Repeated identical commands pair in order

- **GIVEN** a session that ran `ls`, then `pwd`, then `ls` as three Bash calls
- **WHEN** the session view pairs its groups
- **THEN** the first `ls` group shows the first `ls` call and the second shows the second

#### Scenario: Calls that cannot be told apart are marked ambiguous

- **GIVEN** two identical Bash calls the agent made in one turn, so with one time
- **WHEN** the session view lists their groups
- **THEN** each group says it is one of two identical calls made at once, naming both, and neither is paired
