import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { PushNotice } from "./PushNotice";

describe("PushNotice", () => {
  it.each([
    [{ fanout_hosts: 1, fanout_failed: 0 }, "Saved as version 2. Queued for 1 of 1 enrolled host."],
    [{ fanout_hosts: 5, fanout_failed: 2 }, "Saved as version 2. Queued for 3 of 5 enrolled hosts; the rest receive it within minutes."],
  ])("counts the hosts a push reached: %o", (counts, text) => {
    render(<PushNotice version={2} counts={counts} noun="settings" />);
    expect(screen.getByRole("status")).toHaveTextContent(text);
  });

  it("calls out a push that reached no host because the hosts could not be listed", () => {
    render(<PushNotice version={2} counts={{ fanout_hosts: 0, fanout_failed: 0, fanout_skipped_reason: "host_lister_error" }}
      noun="settings" />);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Saved as version 2, but not sent: the enrolled hosts could not be listed. Hosts receive the settings within minutes once they can be.",
    );
  });
});
