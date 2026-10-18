# Read a tool call's command through Claude Code's double-quoted escape

## Why

Claude Code 2.1.300 and later run a Bash tool call as `eval '<command>'` and escape each single quote inside the command as `'"'"'`. The session view read the eval argument without double-quoted runs, so a command containing a single quote was shown garbled (`osascript -e ""quit`), and because the reported tool call pairs by exact command, it went without its "Reported by agent" label (issue #1350). The RSA rerun's `echo "const esbuild = require('esbuild');..."` hit both.

## What changes

The command is read as the shell reads the eval's word: single-quoted runs, double-quoted runs with POSIX's escapes, and backslash escapes, joined. The search for the wrapper's eval skips double-quoted runs as well as single-quoted ones.

## Impact

UI only.
