## ADDED Requirements

### Requirement: A laptop install signs in with the owner's passkey

On a laptop install, the login page SHALL offer one sign-in button, "Sign in with Touch ID", which signs the owner in with their passkey alone, and SHALL NOT show the single sign-on button or the break-glass link. The email, password and passkey form SHALL stay a click away, its email filled in. On a server install, the page SHALL show single sign-on and the break-glass link as before. The page SHALL show no sign-in control until it has read the profile.

#### Scenario: The laptop login page shows one passkey button

- **GIVEN** a laptop install and a browser with no session
- **WHEN** the owner opens the login page and clicks the one button, "Sign in with Touch ID"
- **THEN** the page shows no single sign-on button and no break-glass link
- **AND** the button runs the passkey-only ceremony for the email of the last sign-in in that browser, or with none for any passkey the browser holds for the site, and signs the owner in to the console with no email or password typed

#### Scenario: The email and password form is a click away

- **GIVEN** a laptop install's login page
- **WHEN** the owner chooses "Use your email and password", or a Touch ID sign-in fails
- **THEN** the email, password and passkey form is shown with the email filled in, `admin@rawtrail.local` when the browser remembers none, and a failure says why
- **AND** a sign-in through it is remembered as the email the next Touch ID sign-in uses

#### Scenario: The server login page is unchanged

- **GIVEN** a server install and a browser with no session
- **WHEN** the operator opens the login page
- **THEN** it shows the "Continue with single sign-on" button and the "Break-glass login" link, and no passkey form
