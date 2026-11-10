## ADDED Requirements

### Requirement: Responses are compressed for clients that accept it

The server SHALL compress an HTTP response body with zstd or gzip when the request's `Accept-Encoding` names one of them and the body is large enough to gain from it, and SHALL then mark the response `Vary: Accept-Encoding`. A request that accepts neither SHALL receive the body unencoded.

#### Scenario: A large response is compressed when the client accepts it

- **GIVEN** a JSON response of several hundred kilobytes
- **WHEN** a client requests it with `Accept-Encoding: gzip`, or with a list that includes `zstd`
- **THEN** the response carries `Content-Encoding: gzip` or `zstd` respectively and `Vary: Accept-Encoding`, and decodes to the body the handler wrote

#### Scenario: A client that accepts no encoding gets the body as is

- **GIVEN** the same response, or one of a few bytes
- **WHEN** a client requests the large one with no `Accept-Encoding`, or the small one with `Accept-Encoding: gzip`
- **THEN** the response has no `Content-Encoding` and its body is exactly what the handler wrote
