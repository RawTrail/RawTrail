import { useCallback, useEffect, useRef, useState } from "react";
import {
  DetectionConfigApiError,
  getCredentialGuard,
  replaceCredentialGuard,
  type CredentialGuardMode,
  type CredentialGuardPolicy,
  type CredentialKind,
  type ProtectedCredential,
  type ReplaceCredentialGuardResult,
} from "../../api";
import { formatRelativeISO } from "../../time";
import { Button } from "../ui/Button";
import { Input, Select } from "../ui/Input";
import { Table, EmptyState } from "../ui/Table";
import { PushNotice } from "./PushNotice";
import { ReasonModal } from "./ReasonModal";
import { useReauthDraft } from "../../hooks/useReauthDraft";
import { useReauthRetry } from "../../hooks/useReauthRetry";
import { ReauthDraftNotice } from "../ReauthDraftNotice";
import { ReauthModal } from "../ReauthModal";
import { CREDENTIAL_KINDS, credentialKey, describeOwner, formatOwners, parseOwners } from "./credentialOwners";

const MODE_LABEL: Record<CredentialGuardMode, string> = {
  detect: "Detect: report an agent's read",
  protect: "Protect: refuse an agent's read",
  off: "Off: watch nothing",
};

const MATCH_LABEL: Record<ProtectedCredential["match"], string> = {
  literal: "This file",
  prefix: "Files whose names start with it",
};

const KINDS = [...CREDENTIAL_KINDS.keys()];

function isMode(v: unknown): v is CredentialGuardMode {
  return v === "detect" || v === "protect" || v === "off";
}

function isKind(v: unknown): v is CredentialKind {
  return typeof v === "string" && CREDENTIAL_KINDS.has(v as CredentialKind);
}

// The form for adding a credential, or for one taken out of the list to be edited.
interface CredentialForm {
  readonly kind: CredentialKind;
  readonly path: string;
  readonly match: ProtectedCredential["match"];
  readonly owners: string;
}

const EMPTY_FORM: CredentialForm = { kind: "ssh_private_key", path: "", match: "literal", owners: "" };

// CredentialGuardUnsaved is the section's unsaved work, kept across a reauthentication (issue #1113) as WatchedPaths keeps its own.
interface CredentialGuardUnsaved {
  readonly mode: CredentialGuardMode;
  readonly credentials: ProtectedCredential[];
  readonly baseVersion: number;
  readonly form: CredentialForm;
}

function parseCredential(raw: unknown): ProtectedCredential | undefined {
  if (typeof raw !== "object" || raw === null) return undefined;
  const { kind, path, match, owners, default: builtIn } = raw as Record<string, unknown>;
  if (!isKind(kind) || typeof path !== "string" || (match !== "literal" && match !== "prefix") || !Array.isArray(owners)) {
    return undefined;
  }
  const parsed: ProtectedCredential["owners"] = [];
  for (const o of owners as unknown[]) {
    if (typeof o !== "object" || o === null) return undefined;
    const { team_id, signing_id } = o as Record<string, unknown>;
    if ((team_id !== undefined && typeof team_id !== "string") || (signing_id !== undefined && typeof signing_id !== "string")) {
      return undefined;
    }
    parsed.push({
      ...(team_id === undefined ? {} : { team_id }),
      ...(signing_id === undefined ? {} : { signing_id }),
    });
  }
  return { kind, path, match, owners: parsed, ...(builtIn === true ? { default: true } : {}) };
}

// parseCredentialGuardUnsaved accepts only a payload of this section's own shape, since storage can hold whatever was written under the
// key.
function parseCredentialGuardUnsaved(raw: unknown): CredentialGuardUnsaved | undefined {
  if (typeof raw !== "object" || raw === null) return undefined;
  const { mode, credentials, baseVersion, form } = raw as Record<string, unknown>;
  if (!isMode(mode) || !Array.isArray(credentials) || typeof baseVersion !== "number") return undefined;
  if (typeof form !== "object" || form === null) return undefined;
  const { kind, path, match, owners } = form as Record<string, unknown>;
  if (!isKind(kind) || typeof path !== "string" || (match !== "literal" && match !== "prefix") || typeof owners !== "string") {
    return undefined;
  }
  const list: ProtectedCredential[] = [];
  for (const entry of credentials) {
    const c = parseCredential(entry);
    if (c === undefined) return undefined;
    list.push(c);
  }
  return { mode, credentials: list, baseVersion, form: { kind, path, match, owners } };
}

function samePolicy(mode: CredentialGuardMode, credentials: readonly ProtectedCredential[], stored: CredentialGuardPolicy): boolean {
  return (
    mode === stored.mode &&
    JSON.stringify(credentials.map(credentialKey)) === JSON.stringify(stored.credentials.map(credentialKey))
  );
}

// CredentialGuard edits the credential guard policy (issue #1432): whether every host's extension reports, refuses or ignores an AI
// agent reading a credential file that is not its tool's, which files those are, and the tools allowed to read each. Like the watched
// paths, the operator builds a draft of the whole policy and saves it with a reason; the server validates and pushes it, and its
// refusal names the field.
export function CredentialGuard({ canWrite }: { readonly canWrite: boolean }) {
  const { call: replaceWithReauth, modal: reauthModal } = useReauthRetry(
    useCallback(
      (mode: CredentialGuardMode, credentials: ProtectedCredential[], reason: string, version: number) =>
        replaceCredentialGuard(mode, credentials, reason, version),
      [],
    ),
  );
  const [stored, setStored] = useState<CredentialGuardPolicy | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [mode, setMode] = useState<CredentialGuardMode>("detect");
  const [draft, setDraft] = useState<ProtectedCredential[]>([]);
  const [form, setForm] = useState<CredentialForm>(EMPTY_FORM);
  const [reasonOpen, setReasonOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [conflict, setConflict] = useState(false);
  const [saved, setSaved] = useState<ReplaceCredentialGuardResult | null>(null);
  const [draftRestored, setDraftRestored] = useState(false);
  const [restoredVersion, setRestoredVersion] = useState<number | null>(null);

  const formTouched = form.path !== "" || form.owners !== "";
  const unsaved: CredentialGuardUnsaved | null =
    stored !== null && (!samePolicy(mode, draft, stored) || formTouched)
      ? { mode, credentials: draft, baseVersion: restoredVersion ?? stored.version, form }
      : null;
  const { restored, consume } = useReauthDraft<CredentialGuardUnsaved>("credential-guard", unsaved, parseCredentialGuardUnsaved);
  const restoredOnMount = useRef(restored);

  const adopt = (policy: CredentialGuardPolicy) => {
    setStored(policy);
    setMode(policy.mode);
    setDraft(policy.credentials);
  };

  useEffect(() => {
    let cancelled = false;
    getCredentialGuard()
      .then((policy) => {
        if (cancelled) return;
        setStored(policy);
        const kept = restoredOnMount.current;
        if (kept === undefined) {
          setMode(policy.mode);
          setDraft(policy.credentials);
          return;
        }
        setMode(kept.mode);
        setDraft(kept.credentials);
        setForm(kept.form);
        setRestoredVersion(kept.baseVersion);
        setDraftRestored(true);
        consume();
      })
      .catch((err: unknown) => {
        if (!cancelled) setLoadError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, [consume]);

  const loadLatest = () => {
    getCredentialGuard()
      .then((policy) => {
        adopt(policy);
        setRestoredVersion(null);
        setSaveError(null);
        setConflict(false);
      })
      .catch((err: unknown) => {
        setSaveError(`The latest policy could not be loaded: ${err instanceof Error ? err.message : String(err)}`);
      });
  };

  if (loadError !== null) {
    return <EmptyState>The credential guard policy could not be loaded: {loadError}</EmptyState>;
  }
  if (stored === null) {
    return <EmptyState>Loading the credential guard policy...</EmptyState>;
  }

  const changed = !samePolicy(mode, draft, stored);
  const formEntry: ProtectedCredential = { kind: form.kind, path: form.path, match: form.match, owners: parseOwners(form.owners) };
  const canAdd = form.path.trim() !== "" && !draft.some((c) => c.path === form.path);

  const clearOutcome = () => {
    setDraftRestored(false);
    setSaved(null);
    setSaveError(null);
    setConflict(false);
  };
  const editDraft = (next: ProtectedCredential[]) => {
    setDraft(next);
    clearOutcome();
  };
  const confirmSave = (reason: string) => {
    setSaving(true);
    clearOutcome();
    replaceWithReauth(mode, draft, reason, restoredVersion ?? stored.version)
      .then((result) => {
        adopt(result.policy);
        setRestoredVersion(null);
        setSaved(result);
      })
      .catch((err: unknown) => {
        if (err instanceof DetectionConfigApiError && err.code === "detection_config.conflict") {
          setConflict(true);
          setSaveError(
            "Not saved: someone changed the credential guard after this page loaded it. Load the latest policy to see their change, then make yours again.",
          );
          return;
        }
        setSaveError(`Not saved: ${err instanceof Error ? err.message : String(err)}`);
      })
      .finally(() => {
        setSaving(false);
        setReasonOpen(false);
      });
  };

  return (
    <>
      <ReauthModal {...reauthModal} />
      {draftRestored && <ReauthDraftNotice setChanged={restoredVersion !== null && restoredVersion !== stored.version} />}
      <p className="detection-config__note">
        Each host&apos;s security extension watches these credential files in every user&apos;s home folder. When a process in an AI
        coding agent&apos;s session opens one, and it is not one of the tools allowed to read that file, detect mode reports it and
        protect mode refuses it. Reads outside an agent session are never refused. A tool is allowed by its signature: an Apple tool by
        its identifier (<code>com.apple.ssh</code>), every tool a developer team signs by the team identifier (<code>94KV3E626L</code>),
        or one of the team&apos;s tools as <code>TEAMID:identifier</code>.
      </p>

      <div className="detection-config__form">
        <Select
          label="Mode"
          id="dc-credential-guard-mode"
          inline={false}
          value={mode}
          disabled={!canWrite || saving}
          onChange={(e) => {
            if (isMode(e.target.value)) {
              setMode(e.target.value);
              clearOutcome();
            }
          }}
        >
          {Object.entries(MODE_LABEL).map(([m, label]) => (
            <option key={m} value={m}>
              {label}
            </option>
          ))}
        </Select>
      </div>

      {draft.length === 0 ? (
        <EmptyState>No credentials protected. The guard watches nothing until one is added.</EmptyState>
      ) : (
        <Table>
          <thead>
            <tr>
              <th>Credential</th>
              <th>Path in each home</th>
              <th>Covers</th>
              <th>Tools allowed to read it</th>
              {canWrite && <th aria-label="Actions" />}
            </tr>
          </thead>
          <tbody>
            {draft.map((c, i) => (
              <tr key={credentialKey(c)}>
                <td>
                  {CREDENTIAL_KINDS.get(c.kind)}
                  {c.default === true && <span className="detection-config__badge">Built in</span>}
                </td>
                <td>
                  <code>~/{c.path}</code>
                </td>
                <td>{MATCH_LABEL[c.match]}</td>
                <td>
                  {c.owners.length === 0 ? (
                    "None"
                  ) : (
                    <ul className="detection-config__owners">
                      {c.owners.map((o) => (
                        <li key={describeOwner(o)}>{describeOwner(o)}</li>
                      ))}
                    </ul>
                  )}
                </td>
                {canWrite && (
                  <td>
                    <Button
                      variant="text-link"
                      aria-label={`Edit ~/${c.path}`}
                      disabled={saving || formTouched}
                      onClick={() => {
                        setForm({ kind: c.kind, path: c.path, match: c.match, owners: formatOwners(c.owners) });
                        editDraft(draft.filter((_, j) => j !== i));
                      }}
                    >
                      Edit
                    </Button>
                    <Button
                      variant="text-link"
                      aria-label={`Remove ~/${c.path}`}
                      disabled={saving}
                      onClick={() => {
                        editDraft(draft.filter((_, j) => j !== i));
                      }}
                    >
                      Remove
                    </Button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {stored.updated_at !== undefined && (
        <p className="detection-config__note">
          Last saved {formatRelativeISO(stored.updated_at)} by {stored.updated_by_label ?? stored.updated_by ?? ""}.
        </p>
      )}

      {canWrite && (
        <div className="detection-config__form">
          <Select
            label="Credential"
            id="dc-credential-kind"
            inline={false}
            value={form.kind}
            disabled={saving}
            onChange={(e) => {
              if (isKind(e.target.value)) setForm({ ...form, kind: e.target.value });
            }}
          >
            {KINDS.map((k) => (
              <option key={k} value={k}>
                {CREDENTIAL_KINDS.get(k)}
              </option>
            ))}
          </Select>
          <div className="detection-config__form-field--full">
            <Input
              label="Path in each home"
              id="dc-credential-path"
              value={form.path}
              disabled={saving}
              placeholder=".aws/credentials"
              onChange={(e) => {
                setForm({ ...form, path: e.target.value });
              }}
            />
          </div>
          <Select
            label="Covers"
            id="dc-credential-match"
            inline={false}
            value={form.match}
            disabled={saving}
            onChange={(e) => {
              setForm({ ...form, match: e.target.value === "prefix" ? "prefix" : "literal" });
            }}
          >
            <option value="literal">{MATCH_LABEL.literal}</option>
            <option value="prefix">{MATCH_LABEL.prefix}</option>
          </Select>
          <div className="detection-config__form-field--full">
            <Input
              label="Tools allowed to read it"
              id="dc-credential-owners"
              value={form.owners}
              disabled={saving}
              placeholder="com.apple.ssh, 94KV3E626L, 59GAB85EFG:com.apple.git-remote-http"
              onChange={(e) => {
                setForm({ ...form, owners: e.target.value });
              }}
            />
          </div>
          <div className="detection-config__form-actions">
            <Button
              variant="inverse"
              disabled={!canAdd || saving}
              onClick={() => {
                editDraft([...draft, formEntry]);
                setForm(EMPTY_FORM);
              }}
            >
              Add credential
            </Button>
            <Button
              variant="inverse"
              disabled={(!changed && !formTouched) || saving}
              onClick={() => {
                setMode(stored.mode);
                editDraft(stored.credentials);
                setForm(EMPTY_FORM);
                setRestoredVersion(null);
              }}
            >
              Discard changes
            </Button>
            <Button
              disabled={!changed || formTouched || saving}
              onClick={() => {
                setReasonOpen(true);
              }}
            >
              Save and push to hosts
            </Button>
          </div>
        </div>
      )}
      {saveError !== null && (
        <div className="detection-config__error" role="alert">
          {saveError}
          {conflict && (
            <Button variant="text-link" onClick={loadLatest}>
              Load the latest policy (discards your changes)
            </Button>
          )}
        </div>
      )}
      {saved !== null && <PushNotice version={saved.policy.version} counts={saved} noun="policy" />}
      {reasonOpen && (
        <ReasonModal
          title="Save the credential guard policy?"
          description="The policy replaces the current one on every enrolled host. The change is recorded in the audit log with this reason."
          confirmLabel="Save"
          busy={saving}
          placeholder="Why the guard is changed"
          onConfirm={confirmSave}
          onCancel={() => {
            setReasonOpen(false);
          }}
        />
      )}
    </>
  );
}
