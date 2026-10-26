## ADDED Requirements

### Requirement: Unsaved work survives a reauthentication

When a save needs a fresh sign-in through the identity provider, which leaves the page, the console SHALL keep the page's unsaved work, say so, and put it back on return with a notice. It SHALL NOT save or re-run the action for the operator, who reviews the restored work and saves again. Work that cannot be kept SHALL be said to be lost before the page is left.

#### Scenario: Unsaved work is kept across the sign-in

- **GIVEN** a page with unsaved work and a save that needs a fresh sign-in through the identity provider
- **WHEN** the operator continues to the identity provider
- **THEN** the work is saved in the browser before the page is left
- **AND** the dialog said that the work would be kept and that signing in saves nothing

#### Scenario: Unsaved work is restored and not saved for the operator

- **GIVEN** unsaved work kept across a sign-in
- **WHEN** the operator returns to the page
- **THEN** the work is put back and a notice says it was kept
- **AND** nothing has been saved, and the save is not run again

#### Scenario: Saved work is not restored when it is stale or from another page

- **GIVEN** saved work older than 15 minutes, or saved for another page or another rule
- **WHEN** a page is opened
- **THEN** that work is not restored into it

#### Scenario: Work that cannot be kept is said to be lost

- **GIVEN** a page with unsaved work and a browser that refuses to store it
- **WHEN** the operator continues to the identity provider
- **THEN** the dialog says the work will be lost and does not leave until the operator continues again
