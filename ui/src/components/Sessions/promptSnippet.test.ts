import { describe, it, expect } from "vitest";
import { snippetAround } from "./promptSnippet";

describe("snippetAround", () => {
  it("cuts a long prompt to the text around its first match, regardless of case", () => {
    const text = `${"a".repeat(200)} Deploy to prod ${"b".repeat(200)}`;
    const s = snippetAround(text, "deploy");
    expect(s.match).toBe("Deploy");
    expect(s.before.startsWith("...")).toBe(true);
    expect(s.before.length).toBe(3 + 80);
    expect(s.after.endsWith("...")).toBe(true);
    expect(s.after.length).toBe(80 + 3);
  });

  it("adds no ellipsis where the prompt does not go on", () => {
    expect(snippetAround("deploy it", "DEPLOY")).toEqual({ before: "", match: "deploy", after: " it" });
  });

  it("marks the match in the prompt itself when lowercasing changes the prompt's length", () => {
    expect(snippetAround("İ deploy now", "Deploy")).toEqual({ before: "İ ", match: "deploy", after: " now" });
  });

  it("matches a query's special characters as themselves", () => {
    expect(snippetAround("it is 100% (done) now", "100% (done)")).toEqual({ before: "it is ", match: "100% (done)", after: " now" });
    expect(snippetAround("a.b and axb", "x").match).toBe("x");
  });

  it("shows the prompt's start when the query is not in it", () => {
    expect(snippetAround("run the tests", "deploy")).toEqual({ before: "run the tests", match: "", after: "" });
  });
});
