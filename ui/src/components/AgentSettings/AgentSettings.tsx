import { useCallback, useEffect, useState } from "react";
import {
  DetectionConfigApiError,
  getAgentSettings,
  ReauthRequiredError,
  replaceAgentSettings,
  type AgentSettings as AgentSettingsResponse,
  type ReplaceAgentSettingsResult,
} from "../../api";
import { useReauthRetry } from "../../hooks/useReauthRetry";
import { useCan, PermissionAction } from "../../permissions-core";
import { formatRelativeISO } from "../../time";
import { PushNotice } from "../DetectionConfig/PushNotice";
import { ReasonModal } from "../DetectionConfig/ReasonModal";
import { ReauthModal } from "../ReauthModal";
import { Button } from "../ui/Button";
import { PageHeader } from "../ui/PageHeader";
import { Toggle } from "../ui/Toggle";
import { StorageReport } from "./StorageReport";
import "./AgentSettings.scss";

// AgentSettings is the Data collection settings page (issue #1426): the fleet-wide switch for whether agents capture the prompts people
// type to AI coding agents. It says plainly what is captured while the switch is on, since an upgraded fleet starts capturing without
// anyone choosing to. A change asks for a reason, which the audit row keeps, and is pushed to every enrolled host.
export function AgentSettings() {
  const canWrite = useCan()(PermissionAction.DetectionConfigWrite);
  const { call: replaceWithReauth, modal: reauthModal } = useReauthRetry(
    useCallback(
      (capture: boolean, reason: string, version: number): Promise<ReplaceAgentSettingsResult> =>
        replaceAgentSettings(capture, reason, version),
      [],
    ),
  );
  const [stored, setStored] = useState<AgentSettingsResponse | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  // pending is the value an operator asked to switch to, while the reason dialog is open.
  const [pending, setPending] = useState<boolean | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  // conflict is set when a change was refused because someone else changed the settings since they were loaded.
  const [conflict, setConflict] = useState(false);
  const [saved, setSaved] = useState<ReplaceAgentSettingsResult | null>(null);

  useEffect(() => {
    getAgentSettings()
      .then(setStored)
      .catch((err: unknown) => {
        setLoadError(errorText(err));
      });
  }, []);

  // loadLatest reads the settings again after a change the server refused as out of date, or one whose outcome is unknown, and then
  // says note. A read that fails keeps the page and the offer to try again, since the settings shown may be out of date.
  const loadLatest = (note: string | null) => {
    getAgentSettings()
      .then((settings) => {
        setStored(settings);
        setConflict(false);
        setSaveError(note);
      })
      .catch((err: unknown) => {
        setConflict(true);
        setSaveError(`The latest settings could not be loaded: ${errorText(err)}`);
      });
  };

  if (loadError !== null) {
    return <p className="agent-settings__error" role="alert">The agent settings could not be loaded: {loadError}</p>;
  }
  if (stored === null) return <p className="agent-settings__note">Loading...</p>;

  const confirm = (reason: string) => {
    if (pending === null) return;
    setSaving(true);
    setSaveError(null);
    setSaved(null);
    replaceWithReauth(pending, reason, stored.version)
      .then((result) => {
        setStored(result.settings);
        setSaved(result);
      })
      .catch((err: unknown) => {
        if (err instanceof DetectionConfigApiError && err.code === "detection_config.conflict") {
          setConflict(true);
          setSaveError("Not changed: someone changed the agent settings after this page loaded them. Load the latest, then make your change again.");
          return;
        }
        if (err instanceof DetectionConfigApiError) {
          setSaveError(`Not changed: ${err.message}`);
          return;
        }
        // The server refused the change for want of a fresh sign-in, and the operator did not sign in again.
        if (err instanceof ReauthRequiredError) {
          setSaveError("Not changed: confirm your identity to change this setting.");
          return;
        }
        // No answer from the server, or one this page could not read, says nothing about whether the change was stored.
        loadLatest(`Not confirmed: ${errorText(err)}. The setting shown is what the server holds now.`);
      })
      .finally(() => {
        setSaving(false);
        setPending(null);
      });
  };

  return (
    <section className="agent-settings">
      <ReauthModal {...reauthModal} />
      <PageHeader title="Data collection" />
      <h2 className="agent-settings__title">AI agent prompts</h2>
      <p className="agent-settings__statement" role="status">
        {stored.capture_prompts
          ? "Prompt capture is on. Agents send the prompts people type to AI coding agents such as Claude Code, with recognizable " +
            "credentials replaced before they leave the Mac. Only roles with permission to read prompts can see them."
          : "Prompt capture is off. Agents do not send the prompts people type to AI coding agents."}
      </p>
      <p className="agent-settings__note">
        Commands, tool calls and token usage are sent either way. A change reaches online Macs within seconds and the rest when they
        connect, and a Mac whose agent is configured with <code>EDR_CAPTURE_PROMPTS=0</code> never sends prompts.
      </p>
      <Toggle
        id="agent-settings-capture-prompts"
        label="Capture prompts"
        checked={stored.capture_prompts}
        disabled={!canWrite || saving}
        onChange={() => {
          setPending(!stored.capture_prompts);
        }}
      />
      {stored.updated_at !== undefined && (
        <p className="agent-settings__note">
          Last changed {formatRelativeISO(stored.updated_at)} by {stored.updated_by_label ?? stored.updated_by ?? ""}.
        </p>
      )}
      {saveError !== null && (
        <div className="agent-settings__error" role="alert">
          {saveError}
          {conflict && (
            <Button variant="text-link" onClick={() => { loadLatest(null); }}>
              Load the latest settings
            </Button>
          )}
        </div>
      )}
      {saved !== null && <PushNotice version={saved.settings.version} counts={saved} noun="settings" />}
      <StorageReport />
      {pending !== null && (
        <ReasonModal
          title={pending ? "Turn prompt capture on?" : "Turn prompt capture off?"}
          description={
            pending
              ? "Every enrolled Mac starts sending the prompts people type to AI coding agents. The change is recorded in the audit log with this reason."
              : "Every enrolled Mac stops sending prompts. Prompts already sent are kept until they age out. The change is recorded in the audit log with this reason."
          }
          confirmLabel={pending ? "Turn on" : "Turn off"}
          busy={saving}
          placeholder="Why prompt capture is changing"
          onConfirm={confirm}
          onCancel={() => {
            setPending(null);
          }}
        />
      )}
    </section>
  );
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
