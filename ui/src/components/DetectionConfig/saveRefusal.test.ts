import { describe, expect, it } from "vitest";
import { DetectionConfigApiError } from "../../api";
import { saveRefusal, showSaveRefusal } from "./saveRefusal";

describe("saveRefusal", () => {
  it("gives a version conflict the page's own message and marks it a conflict", () => {
    const err = new DetectionConfigApiError("detection_config.conflict", "version mismatch", 409);
    expect(saveRefusal(err, "Not saved: someone changed it.")).toEqual({ conflict: true, message: "Not saved: someone changed it." });
  });

  it("names any other refusal, from the API or not", () => {
    const invalid = new DetectionConfigApiError("detection_config.invalid", "path must be absolute", 400);
    expect(saveRefusal(invalid, "unused")).toEqual({ conflict: false, message: "Not saved: path must be absolute" });
    expect(saveRefusal(new Error("network down"), "unused")).toEqual({ conflict: false, message: "Not saved: network down" });
    expect(saveRefusal("offline", "unused")).toEqual({ conflict: false, message: "Not saved: offline" });
  });
});

describe("showSaveRefusal", () => {
  it("flags a conflict and shows its message, and shows any other refusal without flagging one", () => {
    const conflicts: boolean[] = [];
    const messages: string[] = [];
    const conflict = new DetectionConfigApiError("detection_config.conflict", "version mismatch", 409);
    showSaveRefusal(
      conflict,
      "Not saved: changed.",
      (c) => conflicts.push(c),
      (m) => messages.push(m),
    );
    showSaveRefusal(
      new Error("network down"),
      "unused",
      (c) => conflicts.push(c),
      (m) => messages.push(m),
    );
    expect(conflicts).toEqual([true]);
    expect(messages).toEqual(["Not saved: changed.", "Not saved: network down"]);
  });
});
