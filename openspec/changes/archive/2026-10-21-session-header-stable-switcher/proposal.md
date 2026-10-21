# Keep the session view switcher in place across views

## Why

The "Show shell plumbing" toggle belongs to the Commands view only. Removing it in Processes and Activity widened the header's subtitle, so its facts line wrapped differently per view and the Commands/Processes/Activity switcher below it moved up or down as the operator switched.

## What changes

The toggle keeps its space in every view and is hidden, not removed, outside Commands. The header lays out the same in all three, and the switcher stays put.

## Impact

UI only.
