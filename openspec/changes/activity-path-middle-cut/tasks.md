## 1. Middle cut

- [x] 1.1 `splitPath` keeps the last two segments as the tail, with vitest over its edge cases
- [x] 1.2 `TruncatedPath` renders a head that gives up its width first and a tail cut at its start only when it alone is too wide

## 2. Width

- [x] 2.1 Replace the 36rem cap with a column that takes the free width, down to a minimum
- [x] 2.2 Playwright: at 1920 pixels the path fills its column and the table does not scroll sideways

## 3. QA

- [x] 3.1 Chrome on lane B (900 pixel viewport, the widest this display gives) and Playwright at 1920 and in the 800 pixel process detail panel
