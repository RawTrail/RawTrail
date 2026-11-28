## 1. Detection

- [x] 1.1 Leave the empty arguments at the end of argv out of Sigma's `CommandLine`, and supply no `CommandLine` when every argument is empty.

## 2. Tests

- [x] 2.1 Field cases: a trailing empty argument, several, an empty argument in the middle, and argv that is all empty.
- [x] 2.2 A negative fixture for `proc_creation_macos_space_after_filename` with iTerm2's `pidinfo` exec, and a positive one for a last argument that ends in a space.

## 3. Verify

- [x] 3.1 Replay the exec shape from the laptop install through the rule on the dev server and confirm it raises no monitor record, while a path ending in a space still does.
