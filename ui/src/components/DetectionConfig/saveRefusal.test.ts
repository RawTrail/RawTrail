import { describe, expect, it } from "vitest";
import { DetectionConfigApiError } from "../../api";
import { saveRefusal } from "./saveRefusal";

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
