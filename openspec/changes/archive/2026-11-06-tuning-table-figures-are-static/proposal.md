## Why

Clicking a value in the detection tuning table's Observed or Cost column revealed a sentence several times wider than the figure, so the column widened and every row reflowed under the reader (#992). Once the worst-case duration is in the Cost cell, that sentence shows nothing the row does not already say, and the Rule cell, which was inert, has an obvious destination in the rule's detail page.

## What changes

- The Cost cell shows the total, the mean and the worst single run. The click-to-expand disclosure is removed from the Observed and Cost columns, with no title or hover replacement.
- The Rule cell links to the rule's detail page.
- The requirement that abbreviated table figures be reachable without a pointer is retired: no cell keeps a precise value elsewhere any more.
- Given up on purpose: the evaluation count, and the exact value behind an abbreviation such as `42k`.

## Impact

- `ui/src/components/DetectionConfig/DetectionConfig.tsx` and `.scss`, and its tests.
