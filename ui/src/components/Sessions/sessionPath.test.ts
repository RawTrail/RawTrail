import { describe, expect, it } from "vitest";

import { sessionPath } from "./sessionPath";

describe("sessionPath", () => {
  it("names the host and the session, each escaped as one path segment", () => {
    expect(sessionPath("H1", "5828cd24-efb7")).toBe("/agent-sessions/H1/5828cd24-efb7");
    expect(sessionPath("h/1", "a b?c")).toBe("/agent-sessions/h%2F1/a%20b%3Fc");
  });
});
