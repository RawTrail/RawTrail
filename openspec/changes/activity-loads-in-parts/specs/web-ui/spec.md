## ADDED Requirements

### Requirement: Activity lists show as they arrive

The Activity tabs SHALL read the scope's totals and each class's first page at once and SHALL show each as it arrives, a class's count once the totals are in. Until the operator picks a class, the first class whose list arrived with rows SHALL be shown, and while none has, the first to arrive.

#### Scenario: A list shows before the totals and the other lists

- **GIVEN** a scope whose totals and Files and Network lists are still being read
- **WHEN** its DNS list arrives
- **THEN** the DNS list is shown, its totals line says it is counting, and the other classes say they are loading
