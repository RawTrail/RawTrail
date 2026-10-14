## 1. Extension

- [x] 1.1 Exec path and arguments read through `esTokenString`.
- [x] 1.2 `tokenString`, the length-bounded read, split out and tested.
- [x] 1.3 The remaining `String(cString:)` reads commented as NUL-terminated C strings.

## 2. QA

- [x] 2.1 edr-dev: exec path and arguments unchanged for ordinary commands, spaces, quotes, Unicode and an empty argument.
