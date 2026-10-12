## 1. Implementation

- [x] 1.1 Write each document through a temporary file created 0600, flushed and renamed; set 0700 on the state directory when it is open to group or other.
- [x] 1.2 Unit test the modes from a world-readable starting point, and mutation-check both mode settings.
- [x] 1.3 Exercise on edr-dev: deploy both extensions, confirm the directory and rewritten files are owner-only and a non-root user is denied, and that the agent still starts and connects to both extensions.
