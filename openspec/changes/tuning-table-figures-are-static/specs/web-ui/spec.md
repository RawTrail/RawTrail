## ADDED Requirements

### Requirement: Tuning table figures are static and a rule names its page

The detection tuning table SHALL show each Observed and Cost figure as plain text that does nothing when clicked, focused or hovered, and the Cost cell SHALL include the worst single evaluation beside the total and the mean. The Rule cell SHALL link to that rule's detail page.

#### Scenario: No figure moves the table

- **GIVEN** the detection tuning table with Observed and Cost figures
- **WHEN** an operator clicks, taps or focuses a figure
- **THEN** nothing is revealed and no column changes width

#### Scenario: The Cost cell carries the worst case

- **GIVEN** a rule with evaluation statistics
- **WHEN** an operator opens the detection tuning view
- **THEN** the rule's Cost cell shows the total, the mean and the worst single evaluation

#### Scenario: The rule opens its detail page

- **GIVEN** a rule in the detection tuning table
- **WHEN** an operator selects the rule's name
- **THEN** the rule's detail page opens

## REMOVED Requirements

### Requirement: Abbreviated table figures are reachable without a pointer

**Reason**: Its only implementation was the click-to-expand figure in the detection tuning table's Observed and Cost columns. Expanding it widened the column and reflowed every row under the reader. Once the worst-case duration moved into the Cost cell the expanded sentence showed nothing the row did not already say, so no table cell keeps a precise value elsewhere and there is nothing left to reach.

**Migration**: None. The Observed and Cost figures are plain text. The evaluation count and the unabbreviated match count are no longer shown anywhere.
