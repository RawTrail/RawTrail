# product-identity Specification

## Purpose

Product identity is the name the product gives itself wherever an operator, an integration or another tool sees it: the web UI, rule credits, exported ATT&CK layers, webhook deliveries and telemetry. It also covers carrying stored data that names the previous product, Fleet EDR, over to RawTrail on upgrade.

## Requirements

### Requirement: The product identifies itself as RawTrail

The product SHALL name itself RawTrail on every surface that names it: the web UI's title and brand mark, the credit on rules it authored, the ATT&CK Navigator layer it exports, the User-Agent of its webhook deliveries (`rawtrail-webhook/1`), and the OpenTelemetry `service.name` of its server (`rawtrail-server`).

#### Scenario: The UI's brand mark reads RawTrail

- **GIVEN** an operator on the sign-in page or any signed-in page
- **WHEN** the page renders
- **THEN** its brand mark reads RawTrail

#### Scenario: A rule this project authored is credited to RawTrail

- **GIVEN** a rule this project authored
- **WHEN** the catalog credits it or it is exported as Sigma
- **THEN** the credit and the exported author are RawTrail

#### Scenario: The Navigator layer is named for RawTrail

- **GIVEN** an operator downloading ATT&CK coverage
- **WHEN** the server builds the Navigator layer
- **THEN** the layer is named "RawTrail coverage"

#### Scenario: A webhook delivery names RawTrail as its sender

- **GIVEN** a configured webhook destination
- **WHEN** the server delivers an event to it
- **THEN** the request carries `User-Agent: rawtrail-webhook/1`

#### Scenario: The server reports telemetry as rawtrail-server

- **GIVEN** a server exporting OpenTelemetry
- **WHEN** it starts
- **THEN** its resource `service.name` is `rawtrail-server`

### Requirement: The break-glass account moves to the RawTrail address on upgrade

The seeded break-glass admin's well-known address SHALL be `admin@rawtrail.local`. Upgrading a server whose break-glass account was seeded at `admin@fleet-edr.local` SHALL move that account to the new address, keeping its identity, credentials and role, rather than seeding a second break-glass account.

#### Scenario: An upgraded server keeps one break-glass account

- **GIVEN** a database whose break-glass admin was seeded at `admin@fleet-edr.local`
- **WHEN** the upgraded server applies its migrations and seeds the break-glass admin
- **THEN** the existing account is found at `admin@rawtrail.local` with its original id
- **AND** exactly one break-glass account exists

### Requirement: Existing alerts are credited to RawTrail on upgrade

Upgrading a server SHALL re-credit every stored alert whose credit names the previous product, "Fleet EDR", to "RawTrail". Alerts credited to an upstream author, and alerts raised before attribution was recorded, SHALL keep their credit.

#### Scenario: An upgrade re-credits only this project's alerts

- **GIVEN** stored alerts credited "Fleet EDR", credited to an upstream author, and carrying no credit
- **WHEN** the upgraded server starts
- **THEN** the first is credited "RawTrail"
- **AND** the other two are unchanged
