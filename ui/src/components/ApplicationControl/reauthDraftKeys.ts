// The keys the application control dialogs keep unsaved work under across an OIDC reauthentication (issue #1113). They live apart from
// the dialogs because the page that opens a dialog has to know whether work is waiting for it, before the dialog reads it.

import { hasReauthDraft } from "../../reauthDraft";

// A dialog's work is keyed by what it acts on: the policy a rule is added to, or the rule being edited, so work on one is never put
// into another's dialog.
export const addRuleDraftKey = (policyID: number): string => `appcontrol-add-rule:${String(policyID)}`;
export const pasteManyDraftKey = (policyID: number): string => `appcontrol-paste-many:${String(policyID)}`;
export const editRuleDraftKey = (ruleID: number): string => `appcontrol-edit-rule:${String(ruleID)}`;

// savedDialogFor says which dialog of a policy page had unsaved work saved before a reauthentication, so the page reopens it.
export function savedDialogFor(policyID: number): "add" | "paste-many" | null {
  if (hasReauthDraft(addRuleDraftKey(policyID))) return "add";
  if (hasReauthDraft(pasteManyDraftKey(policyID))) return "paste-many";
  return null;
}
