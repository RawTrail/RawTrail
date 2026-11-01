import { useEffect, useMemo, useState } from "react";
import "./SessionSummary.scss";
import { getHostDetail } from "../../api";
import type { AgentSessionTotals, Alert } from "../../types";
import { Button } from "../ui/Button";
import { EmptyState } from "../ui/Table";
import { scopeNote } from "../Activity/activityScope";
import { plural } from "../Search/format";
import { altText, shareCard } from "./shareCard";
import { shareSvg, svgDataUrl } from "./shareImage";
import { homeUsernames } from "./shareRedact";
import { SharePanel } from "./SharePanel";

interface SessionSummaryProps {
  readonly hostId: string;
  // alerts are the session's, read for its views; the card says what the credential guard stopped from them.
  readonly alerts: readonly Alert[];
  // totals is the session's summary as the server counted it; undefined when it could not be read.
  readonly totals: AgentSessionTotals | undefined;
  readonly agent: string;
  readonly project: string | undefined;
  readonly firstNs: number | undefined;
  readonly lastNs: number | undefined;
  // onAlerts opens the view the session's alerts are badged in.
  readonly onAlerts: () => void;
}

// HostName is the host's name as the Summary read it: needed before the card is drawn, since it is a name redaction must remove.
type HostName = { state: "loading" } | { state: "read"; hostname: string } | { state: "failed" };

// SessionSummary shows a session as the card it is shared as (issues #1429, #1430): what the agent's log reports beside what actually
// ran, what it changed and where it reached, the programs it ran most and where it talked to. The card shown is the image shared, so
// the two cannot differ. Beside it are the ways to share it, the session's alerts as a count with a link, and what the card leaves out.
export function SessionSummary({ hostId, alerts, totals, agent, project, firstNs, lastNs, onAlerts }: SessionSummaryProps) {
  const [host, setHost] = useState<HostName>({ state: "loading" });
  useEffect(() => {
    let live = true;
    getHostDetail(hostId)
      // A host with no enrolled name is read as unreadable: with no name to remove, nothing can be shared.
      .then((d) => { if (live) setHost(d.hostname.trim() ? { state: "read", hostname: d.hostname.trim() } : { state: "failed" }); })
      .catch(() => { if (live) setHost({ state: "failed" }); });
    return () => { live = false; };
  }, [hostId]);
  // The card is drawn once the host's name is known or known to be unreadable: a host whose name cannot be read still has a Summary,
  // and nothing is offered to share it.
  const card = useMemo(() => {
    if (!totals?.digest || host.state === "loading") return null;
    const usernames = homeUsernames(project, ...totals.digest.directories.map((d) => d.directory),
      ...totals.digest.programs.map((p) => p.program));
    const identities = { usernames, hostnames: host.state === "read" ? [host.hostname] : [] };
    return shareCard({ totals, alerts, agent, project, firstNs, lastNs, identities });
  }, [host, totals, alerts, agent, project, firstNs, lastNs]);
  const svg = useMemo(() => (card ? shareSvg(card) : null), [card]);
  if (!totals?.digest) return <EmptyState>The session&apos;s summary could not be read.</EmptyState>;
  const partial = totals.scope && scopeNote(totals.scope);
  const agentFiles = totals.digest.files.agent_files;
  return (
    <div className="session-summary">
      {partial && <p className="session-summary__note" role="status">{partial}</p>}
      <figure className="session-summary__card">
        {svg && card
          ? <img src={svgDataUrl(svg)} alt={altText(card)} className="session-summary__image" />
          : <div className="session-summary__image" aria-busy="true" />}
      </figure>
      <div className="session-summary__aside">
        <SharePanel card={card} svg={svg} hostUnread={host.state === "failed"} />
        <section className="session-summary__section" aria-label="Alerts">
          <h2 className="session-summary__title">Alerts</h2>
          {totals.alerts.total === 0
            ? <p>No alerts were raised in this session.</p>
            : (
              <p>
                {`${plural(totals.alerts.total, "alert was", "alerts were")} raised in this session. `}
                <Button variant="text-link" onClick={onAlerts}>See them in Commands</Button>
              </p>
            )}
        </section>
        {agentFiles > 0 && (
          <p className="session-summary__note">
            {`Not counted: ${plural(agentFiles, "file", "files")} Claude Code keeps for itself, its transcripts, settings and command `}
            output. The Activity view lists every file.
          </p>
        )}
      </div>
    </div>
  );
}
