// ReauthDraftNotice tells an operator that the work in front of them was kept across a reauthentication, and that it has not been
// saved: the action that needed the fresh sign-in was not run again (issue #1113). A surface adds what else changed while they were
// away, as children.

import type { ReactNode } from "react";
import "./ReauthDraftNotice.scss";

export function ReauthDraftNotice({ children }: Readonly<{ children?: ReactNode }>) {
  return (
    <p className="reauth-draft-notice" role="status">
      Your unsaved changes were kept while you signed in again. Review them, then save again: nothing was saved by signing in.
      {children !== undefined && <> {children}</>}
    </p>
  );
}
