// ReauthDraftNotice tells an operator that the work in front of them was kept across a reauthentication, and that it has not been
// saved: the action that needed the fresh sign-in was not run again (issue #1113).

import "./ReauthDraftNotice.scss";

// setChanged adds that the shared setting the draft was edited against moved while the operator was away, which a save is refused for.
export function ReauthDraftNotice({ setChanged = false }: Readonly<{ setChanged?: boolean }>) {
  return (
    <p className="reauth-draft-notice" role="status">
      Your unsaved changes were kept while you signed in again. Review them, then save again: nothing was saved by signing in.
      {setChanged &&
        " The set was changed by someone else while you were away, so saving will be refused until you load the latest."}
    </p>
  );
}
