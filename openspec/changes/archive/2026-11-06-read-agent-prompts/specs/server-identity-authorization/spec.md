## ADDED Requirements

### Requirement: Reading AI agent prompts is its own permission

Reading the prompts a person typed to an AI agent SHALL require `agent_prompt.read`, which process read SHALL NOT imply. It SHALL be granted to `super_admin`, `admin` and `senior_analyst`, and SHALL NOT be granted to `analyst` or `auditor`. It SHALL NOT be a sampled read: every allowed read SHALL be audited synchronously.

#### Scenario: An analyst cannot read prompts

- **GIVEN** an operator bound to the `analyst` role, which holds process read
- **WHEN** they ask for an agent session's prompts
- **THEN** the read is denied

#### Scenario: Every prompt read is audited

- **WHEN** an operator with `agent_prompt.read` reads a session's prompts
- **THEN** the read is audited synchronously, never sampled away
