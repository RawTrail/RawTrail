## ADDED Requirements

### Requirement: Tool command quoting

The session view SHALL read a tool call's command from its wrapper shell as the shell reads the eval's word: single-quoted runs, double-quoted runs with the escapes POSIX applies inside double quotes, and backslash escapes, joined, so that a command containing an escaped single quote is shown and paired with its reported tool call as the agent ran it.

#### Scenario: A command with the double-quoted escape reads as the agent ran it

- **GIVEN** a wrapper whose eval argument is `'open -b com.apple.TextEdit && sleep 5 && osascript -e '"'"'quit app "TextEdit"'"'"''`
- **WHEN** the session view reads its command
- **THEN** it reads `open -b com.apple.TextEdit && sleep 5 && osascript -e 'quit app "TextEdit"'`, and the escape at the start, middle or end of a command reads the same way
