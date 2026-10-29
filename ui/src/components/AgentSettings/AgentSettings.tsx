import { useCallback, useEffect, useState } from "react";
import {
  DetectionConfigApiError,
  getAgentSettings,
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

  const load = useCallback(() => {
    getAgentSettings()
      .then((settings) => {
        setStored(settings);
        setSaveError(null);
        setConflict(false);
      })
      .catch((err: unknown) => {
        setLoadError(err instanceof Error ? err.message : String(err));
      });
  }, []);
  useEffect(load, [load]);

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
        setSaveError(`Not changed: ${err instanceof Error ? err.message : String(err)}`);
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
            <Button variant="text-link" onClick={load}>
              Load the latest settings
            </Button>
          )}
        </div>
      )}
      {saved !== null && <PushNotice version={saved.settings.version} counts={saved} noun="settings" />}
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
