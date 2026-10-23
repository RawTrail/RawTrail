## Why

A long file path stretched the Activity Files table well past its panel, and nothing said the table scrolled sideways (dogfood, v0.8.0-rc.2). A Claude Code plugin's rename, `<dir>/21989.tmp.c22cddba → <dir>/21989` under a 90-character directory, was over 200 characters on one line.

## What changes

- A file path is capped in width and cut at its start, so its file name stays in view, with the whole path as its tooltip.
- A rename's two paths each take half the cap, so the row stays one line and both file names show.
- The cell's text is still the whole path, so copying a row or searching the page finds it.

## Impact

- `ui/src/components/Activity/ActivityTabs.tsx` and `.scss`.
