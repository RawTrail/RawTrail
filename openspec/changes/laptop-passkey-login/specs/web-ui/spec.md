## ADDED Requirements

### Requirement: A laptop install signs in with the owner's passkey

On a laptop install, the login page SHALL show only the passkey sign-in form, with one button, "Sign in with your passkey", and SHALL NOT show the single sign-on button or the break-glass link. On a server install, the page SHALL show single sign-on and the break-glass link as before. The page SHALL show no sign-in control until it has read the profile.

#### Scenario: The laptop login page shows one passkey button

- **GIVEN** a laptop install and a browser with no session
- **WHEN** the owner opens the login page and submits their email and password with the one button
- **THEN** the page shows no single sign-on button and no break-glass link
- **AND** the button runs the passkey ceremony and signs the owner in to the console

#### Scenario: The server login page is unchanged

- **GIVEN** a server install and a browser with no session
- **WHEN** the operator opens the login page
- **THEN** it shows the "Continue with single sign-on" button and the "Break-glass login" link, and no passkey form
