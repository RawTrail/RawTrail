## MODIFIED Requirements

### Requirement: Agent prompt search

The Agent sessions page SHALL offer an operator holding `agent_prompt.read` a search of the prompts typed across every agent session, over the last day, week or 30 days, listing each prompt newest first with how long ago it was typed, its host, its session and the text around its match marked, a page at a time in the window first searched, each linking to its session opened on its Timeline grouped by prompt. It SHALL not be offered without `agent_prompt.read`.

#### Scenario: An operator finds a prompt across sessions

- **GIVEN** a session one of whose prompts holds a word no other prompt does
- **WHEN** an operator searches for that word in another case
- **THEN** the prompt alone is listed with its host and the word marked
- **AND** its link opens the session on its Timeline grouped by prompt, showing the prompt

#### Scenario: Only an operator who may read prompts can search them

- **GIVEN** an operator holding process read but not `agent_prompt.read`
- **WHEN** they open the Agent sessions page
- **THEN** no prompt search is offered, and one is once they hold `agent_prompt.read`

#### Scenario: Each prompt says when it was typed and which session it is in

- **GIVEN** a search that finds two prompts from one session and one from another
- **WHEN** the results are listed
- **THEN** each prompt's time reads as an age ("6m ago"), with the exact time on hover
- **AND** each names its session by the first 8 characters of its id, as the sessions list does, with the full id on hover and a link to the session, so the two prompts from one session show the same id
