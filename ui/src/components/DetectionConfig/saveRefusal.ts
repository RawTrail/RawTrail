import { DetectionConfigApiError } from "../../api";

// SaveRefusal is why a detection-config save did not land, as the page shows it.
export interface SaveRefusal {
  // conflict is set when someone saved after this page loaded, so the page offers to load the latest.
  conflict: boolean;
  message: string;
}

// saveRefusal reads a failed save: a version conflict gets conflictMessage, which names what changed, and anything else says it was not
// saved and why.
export function saveRefusal(err: unknown, conflictMessage: string): SaveRefusal {
  if (err instanceof DetectionConfigApiError && err.code === "detection_config.conflict") {
    return { conflict: true, message: conflictMessage };
  }
  return { conflict: false, message: `Not saved: ${err instanceof Error ? err.message : String(err)}` };
}
