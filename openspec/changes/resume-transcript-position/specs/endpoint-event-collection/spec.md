## ADDED Requirements

### Requirement: A resumed transcript is read from where it stopped

The agent SHALL keep a transcript's read position while the transcript is idle, and when its session writes to it again SHALL read only what was added, not the transcript from its start. A transcript replaced at its path SHALL be read from its start. The positions kept SHALL be bounded in number, the least recently changed forgotten first.

#### Scenario: A resumed session sends only what it added

- **GIVEN** a transcript the agent has read, whose session then goes idle for longer than the window in which the agent reads transcripts
- **WHEN** the session resumes and writes a new tool call
- **THEN** the agent sends that call and does not send the calls it sent before the break
- **AND** a restart of the agent still reads each recent transcript once from its start, which the archive keeps once by each event's stable id

#### Scenario: The positions kept are bounded

- **GIVEN** more transcripts with a kept position than the bound allows
- **WHEN** the agent finishes a pass over them
- **THEN** it forgets the least recently changed beyond the bound
- **AND** an idle transcript keeps where it was read to, its turn and its permission mode, and drops the record of the streamed messages it sent, which a resumed session does not write again
