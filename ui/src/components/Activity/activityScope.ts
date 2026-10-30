import type { ActivityScope } from "../../types";
import { plural } from "../Search/format";

// scopeNote says how much of the scope the activity reflects, or null when it reflects all of it.
export function scopeNote(scope: ActivityScope): string | null {
  // A truncated scope comes first: when the processes read carry no generation, the unread ones still might, so all that can be said
  // is that the activity is partial.
  if (scope.truncated) return "This scope has more processes than are read, so its activity is partial.";
  if (!scope.applied) {
    return scope.processes === 0
      ? "No stored process matches this scope, so there is no activity to show."
      : "None of this scope's processes carry a process generation, so their activity cannot be read.";
  }
  if (scope.unmatched > 0) {
    return `${plural(scope.unmatched, "process carries", "processes carry")} no process generation, so their activity is not counted.`;
  }
  return null;
}
