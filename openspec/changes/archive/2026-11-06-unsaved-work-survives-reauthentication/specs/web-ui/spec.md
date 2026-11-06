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
- **AND** a restored edit of a shared setting is saved against the version it was edited against, so a change made meanwhile by someone else is refused rather than overwritten

#### Scenario: Saved work is not restored when it is stale or from another page

- **GIVEN** saved work older than 15 minutes, saved for another page or another rule, written by a different operator, or not of the page's own shape
- **WHEN** a page is opened
- **THEN** that work is not restored into it

#### Scenario: Work that cannot be kept is said to be lost

- **GIVEN** a page with unsaved work and a browser that refuses to store it
- **WHEN** the operator continues to the identity provider
- **THEN** the dialog says the work will be lost and does not leave until the operator continues again

#### Scenario: A dialog with saved work is reopened by its page

- **GIVEN** unsaved work in a dialog, such as adding or editing an application control rule, kept across a sign-in
- **WHEN** the operator returns to the page
- **THEN** the page reopens that dialog, and only that dialog
- **AND** the work is put back as it opens, with a notice, and a later close and reopen starts empty as it always did

#### Scenario: A restored edit keeps what someone else changed meanwhile

- **GIVEN** an edit of an existing item kept across a sign-in, in which the operator changed some fields and left others alone
- **WHEN** someone else changes a field the operator left alone before the operator returns
- **THEN** the restored edit contains the operator's changes and the item's current value for the field they left alone
- **AND** saving it does not write the old value back
