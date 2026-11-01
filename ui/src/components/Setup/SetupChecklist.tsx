import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router";
import "./SetupChecklist.scss";
import { getHostHealth, listAgentSessions, listHosts } from "../../api";
import { PageHeader } from "../ui/PageHeader";
import { Badge, type BadgeVariant } from "../ui/Badge";
import { latestMac, setupSteps, type SetupInputs, type StepState } from "./setupSteps";
import { PermissionAction, useCan } from "../../permissions-core";

// POLL_MS is how often the checklist rereads the Mac while a step is open. The agent reports its health every 30 seconds, so a faster
// read would show nothing new.
export const POLL_MS = 10_000;

const STATE_BADGE: Record<StepState, { variant: BadgeVariant; label: string }> = {
  done: { variant: "success", label: "Done" },
  missing: { variant: "critical", label: "Action needed" },
  waiting: { variant: "neutral", label: "Waiting" },
};

// readMac reads what the checklist shows. A Mac named by ?host= is that one, and otherwise the Mac that reported last. Its health and
// sessions are read best effort: a failed read leaves those steps waiting rather than failing the page. Sessions are read only by an
// operator who may read them.
async function readMac(wanted: string | null, canReadSessions: boolean): Promise<SetupInputs> {
  const hosts = await listHosts();
  const host = wanted ? (hosts.find((h) => h.host_id === wanted) ?? null) : latestMac(hosts);
  if (!host) return { host: null, health: null, sessions: null, canReadSessions };
  const [health, sessions] = await Promise.all([
    getHostHealth(host.host_id).catch(() => null),
    canReadSessions ? listAgentSessions({ host_id: host.host_id }).then((r) => r.total_matched, () => null) : null,
  ]);
  return { host, health, sessions, canReadSessions };
}

// SetupChecklist is the first-run checklist (issue #1436): the approvals a Mac needs before it captures, in order, each with where to
// make it, read live from the agent's health. A refused Full Disk Access grant shows red instead of leaving the extension looping.
export function SetupChecklist() {
  const [params] = useSearchParams();
  const wanted = params.get("host");
  const canReadSessions = useCan()(PermissionAction.ProcessRead);
  const [inputs, setInputs] = useState<SetupInputs | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const load = () => {
      readMac(wanted, canReadSessions)
        .then((read) => {
          if (cancelled) return;
          setInputs(read);
          setFailed(false);
          if (setupSteps(read).some((s) => s.state !== "done")) timer = setTimeout(load, POLL_MS);
        })
        .catch(() => {
          if (cancelled) return;
          setFailed(true);
          timer = setTimeout(load, POLL_MS);
        });
    };
    load();
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [wanted, canReadSessions]);

  const steps = inputs ? setupSteps(inputs) : [];
  const remaining = steps.filter((s) => s.state !== "done").length;
  const host = inputs?.host;
  const subtitle = host ? host.hostname || host.host_id : "Waiting for this Mac to enroll";

  return (
    <div className="setup-checklist">
      <PageHeader title="Set up this Mac" subtitle={inputs ? subtitle : undefined} />
      {failed && (
        <p className="form-error" role="alert">
          Could not read this Mac&apos;s status. Retrying.
        </p>
      )}
      {inputs && (
        <>
          <output className="setup-checklist__summary">
            {remaining === 0 ? (
              <>
                Everything is set up. <Link to="/agent-sessions">Go to Agent sessions</Link>
              </>
            ) : (
              `${String(remaining)} of ${String(steps.length)} steps left. This page updates as you go.`
            )}
          </output>
          <ol className="setup-checklist__steps" aria-label="Setup steps">
            {steps.map((step) => (
              <li key={step.id} className={`setup-checklist__step setup-checklist__step--${step.state}`}>
                <Badge variant={STATE_BADGE[step.state].variant}>{STATE_BADGE[step.state].label}</Badge>
                <div>
                  <h2 className="setup-checklist__title">{step.title}</h2>
                  {step.state !== "done" && <p className="setup-checklist__instruction">{step.instruction}</p>}
                </div>
              </li>
            ))}
          </ol>
        </>
      )}
    </div>
  );
}
