## Why

The cap added in rc.3 applied to the path alone, so a socket daemon's name was appended after a full-width path and pushed the cell past it, run into the path with no space: `/Users/victor/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock1Password SSH agent` (dogfood, v0.8.0-rc.3).

## What changes

- A file row's cell is one capped row holding its path, or a rename's two, and its tags. The paths shorten to make room; a tag, such as a socket's daemon, keeps its width.
- The daemon's name is a tag (a pill) beside the path.
- A copy of the cell keeps a space between its parts.

## Impact

- `ui/src/components/Activity/ActivityTabs.tsx` and `.scss`.
