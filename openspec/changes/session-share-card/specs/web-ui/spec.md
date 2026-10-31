## ADDED Requirements

### Requirement: A shared session image carries no identifying detail

A session shared as an image SHALL be drawn only from a card of counts, program names without arguments and domains, every string on which has usernames, hostnames, IP addresses and user@host pairs removed and home folders shown as `~`. An address SHALL never appear as a domain, and the title, project name and internal domains SHALL appear only when the person chooses them.

#### Scenario: What names a person or a machine is removed

- **GIVEN** text holding a home folder, a user@host pair, IPv4 and IPv6 addresses, an internal hostname, and the session's own username and hostname
- **WHEN** it is redacted
- **THEN** the home folder reads `~` and each of the others is removed, while a longer word containing the username is left alone

#### Scenario: The card shows only what the person chose

- **GIVEN** a session in a project, with programs run by full path, public, internal and address domains, and a read the credential guard stopped
- **WHEN** its card is built with the default choices
- **THEN** it shows no title and no project, programs by name only, public domains only, and the stopped read in the sharer's words

#### Scenario: No username, hostname or address survives

- **GIVEN** any session whose agent name, project, programs, domains and chosen title carry its username, hostname and IP addresses
- **WHEN** its card, caption and alt text are built with any choices
- **THEN** none of them contains the username, the hostname, its first label or any of the addresses

### Requirement: A session can be shared as an image made in the browser

The Summary tab SHALL offer to share the session as a PNG card, 1080 x 1350 or 1200 x 675 drawn at twice that size, made in the browser from the redacted card with no request, previewed exactly as exported, with the person's choices applied live. It SHALL copy the image or download it, copy a suggested caption, and open the LinkedIn or X composer, SHALL refuse while the host's name is unread or empty, and SHALL warn when no home folder names the user a title could mention.

#### Scenario: The preview is the exported image, and choices change it

- **GIVEN** a session's Summary with the Share dialog open
- **WHEN** the person turns on the project name or switches to the landscape size
- **THEN** the preview's image and its alt text change to match, and the image exported is the one previewed

#### Scenario: The image is drawn in the browser without a request

- **GIVEN** a card
- **WHEN** it is exported
- **THEN** its SVG is drawn from a data URL onto a canvas twice the card's size and saved as a PNG, and no network request is made

#### Scenario: The image is copied or downloaded, and the caption copied

- **GIVEN** the Share dialog with its preview drawn
- **WHEN** the person copies the image, downloads it, or copies the caption
- **THEN** the clipboard receives an `image/png` item, a PNG file is saved, or the clipboard receives the caption, and the dialog says which

#### Scenario: A host whose name cannot be read is not shared

- **GIVEN** a host whose name the dialog cannot read
- **WHEN** the Share dialog opens
- **THEN** it says the image cannot be checked for the name, draws no preview, and offers nothing to copy, download or post

#### Scenario: A title is checked for the user's name, or the person is warned

- **GIVEN** a session whose project names no home folder
- **WHEN** the person types a title naming the user, and a program or the title itself names the user's home folder
- **THEN** the name is removed from the image; and when nothing names a home folder, the dialog warns that a username in the title cannot be recognized
