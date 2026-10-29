import { useId, type ReactNode } from "react";
import "./SessionSummary.scss";
import type { AgentSessionDigest, AgentSessionTotals } from "../../types";
import { Badge } from "../ui/Badge";
import { Button } from "../ui/Button";
import { EmptyState } from "../ui/Table";
import { formatNs, plural } from "../Search/format";
import { BYPASS_MODE, formatSpan, formatTokens, usageLine } from "./sessionUsage";
import { barHeights, directoryLabel, fileOperations, groupDomains } from "./sessionDigest";

interface SessionSummaryProps {
  // totals is the session's summary as the server counted it; undefined when it could not be read.
  readonly totals: AgentSessionTotals | undefined;
  readonly agent: string;
  readonly project: string | undefined;
  readonly firstNs: number | undefined;
  readonly lastNs: number | undefined;
  // onAlerts opens the view the session's alerts are badged in.
  readonly onAlerts: () => void;
}

// SessionSummary describes a session at a glance (issue #1429): what it was asked and ran, what it used, what it touched, where it
// talked to, what it cost in tokens, and when it was busy. It is built on the summary endpoint alone, so it reads the whole session,
// not a page of it. The tone is descriptive: alerts are a count with a link, not the headline.
export function SessionSummary({ totals, agent, project, firstNs, lastNs, onAlerts }: SessionSummaryProps) {
  const digest = totals?.digest;
  if (!totals || !digest) return <EmptyState>The session&apos;s summary could not be read.</EmptyState>;
  return (
    <div className="session-summary">
      <SummarySection title="At a glance">
        <ul className="session-summary__facts">
          <li>{agent}</li>
          {project && <li className="session-summary__mono">{project}</li>}
          {firstNs !== undefined && lastNs !== undefined && <li>{`Ran for ${formatSpan(lastNs - firstNs)}`}</li>}
          <li>{plural(digest.prompts, "prompt", "prompts")}</li>
          <li>{plural(totals.commands, "command", "commands")}</li>
        </ul>
      </SummarySection>
      <SummaryUsed totals={totals} digest={digest} />
      <SummaryTouched totals={totals} digest={digest} project={project} />
      <SummaryTalked totals={totals} digest={digest} />
      <SummaryEffort totals={totals} />
      <SummaryTimeline digest={digest} lastNs={lastNs} />
      <SummarySection title="Alerts">
        {totals.alerts.total === 0
          ? <p>No alerts were raised in this session.</p>
          : (
            <p>
              {`${plural(totals.alerts.total, "alert was", "alerts were")} raised in this session. `}
              <Button variant="text-link" onClick={onAlerts}>See them in Commands</Button>
            </p>
          )}
      </SummarySection>
    </div>
  );
}

function SummarySection({ title, children }: { readonly title: string; readonly children: ReactNode }) {
  const id = useId();
  return (
    <section className="session-summary__section" aria-labelledby={id}>
      <h2 id={id} className="session-summary__title">{title}</h2>
      {children}
    </section>
  );
}

// CountList lists named counts, most first as the server ordered them.
function CountList({ label, rows }: { readonly label: string; readonly rows: readonly { name: string; count: number }[] }) {
  return (
    <ul className="session-summary__counts" aria-label={label}>
      {rows.map((r) => (
        <li key={r.name}>
          <span className="session-summary__mono">{r.name}</span>
          <span className="session-summary__count">{r.count.toLocaleString()}</span>
        </li>
      ))}
    </ul>
  );
}

function SummaryUsed({ totals, digest }: { readonly totals: AgentSessionTotals; readonly digest: AgentSessionDigest }) {
  const tools = totals.tool_calls.by_tool;
  return (
    <SummarySection title="What it used">
      <div className="session-summary__columns">
        <div>
          <h3 className="session-summary__subtitle">{plural(totals.tool_calls.total, "tool call", "tool calls")}</h3>
          {tools.length === 0
            ? <p>No tool calls were recorded.</p>
            : <CountList label="Tool calls by tool" rows={tools.map((t) => ({ name: t.tool, count: t.count }))} />}
        </div>
        <div>
          <h3 className="session-summary__subtitle">Programs it ran most</h3>
          {digest.programs.length === 0
            ? <p>Its commands ran no programs.</p>
            : <CountList label="Programs by processes run" rows={digest.programs.map((p) => ({ name: p.program, count: p.count }))} />}
        </div>
      </div>
    </SummarySection>
  );
}

function SummaryTouched({ totals, digest, project }: {
  readonly totals: AgentSessionTotals;
  readonly digest: AgentSessionDigest;
  readonly project: string | undefined;
}) {
  const files = totals.activity?.files;
  if (!files || files.events === 0) {
    return <SummarySection title="What it touched"><p>No file activity was recorded.</p></SummarySection>;
  }
  const operations = fileOperations(files.by_operation).map((o) => `${o.count.toLocaleString()} ${o.label}`);
  const across = `${plural(files.events, "file event", "file events")} across ${plural(files.distinct, "path", "paths")}`;
  return (
    <SummarySection title="What it touched">
      <p>{`${across}: ${operations.join(", ")}.`}</p>
      {project && (
        <p>
          {digest.outside_project === 0
            ? "Nothing outside this project."
            : `${plural(digest.outside_project, "path", "paths")} outside this project.`}
        </p>
      )}
      <CountList
        label="File events by directory"
        rows={digest.directories.map((d) => ({ name: directoryLabel(d.directory, project), count: sumOf(d.by_operation) }))}
      />
      {digest.directories_truncated && <p className="session-summary__note">The busiest directories are listed.</p>}
    </SummarySection>
  );
}

function sumOf(counts: Readonly<Record<string, number>>): number {
  return Object.values(counts).reduce((a, b) => a + b, 0);
}

function SummaryTalked({ totals, digest }: { readonly totals: AgentSessionTotals; readonly digest: AgentSessionDigest }) {
  const network = totals.activity?.network.events ?? 0;
  const dns = totals.activity?.dns.events ?? 0;
  if (network === 0 && dns === 0) {
    return <SummarySection title="Where it talked to"><p>No network activity was recorded.</p></SummarySection>;
  }
  const fresh = digest.domains.filter((d) => d.new).length;
  return (
    <SummarySection title="Where it talked to">
      <p>
        {`${plural(dns, "DNS lookup", "DNS lookups")}, ${plural(network, "connection", "connections")}`}
        {fresh > 0 && `, ${plural(fresh, "domain", "domains")} this host had not looked up before`}
        .
      </p>
      {groupDomains(digest.domains).map((group) => (
        <div key={group.label} className="session-summary__group">
          <h3 className="session-summary__subtitle">{group.label}</h3>
          <ul className="session-summary__counts" aria-label={group.label}>
            {group.domains.map((d) => (
              <li key={d.domain}>
                <span className="session-summary__mono">{d.domain}</span>
                {d.new && <Badge variant="neutral">New to this host</Badge>}
                <span className="session-summary__count">
                  {[d.lookups > 0 && plural(d.lookups, "lookup", "lookups"), d.connections > 0 && plural(d.connections, "connection",
                    "connections")].filter(Boolean).join(", ")}
                </span>
              </li>
            ))}
          </ul>
        </div>
      ))}
      {digest.domains_truncated && <p className="session-summary__note">The busiest domains are listed.</p>}
    </SummarySection>
  );
}

function SummaryEffort({ totals }: { readonly totals: AgentSessionTotals }) {
  const usage = usageLine(totals.usage);
  if (!usage) return <SummarySection title="Effort"><p>No token usage was recorded.</p></SummarySection>;
  return (
    <SummarySection title="Effort">
      <p>
        {`${formatTokens(usage.total)} tokens: ${formatTokens(usage.input)} in, ${formatTokens(usage.cache)} through the prompt cache, `}
        {`${formatTokens(usage.output)} out, on ${usage.models.join(", ")}.`}
      </p>
      <ul className="session-summary__counts" aria-label="Time by permission mode">
        {usage.modes.map((m) => (
          <li key={m.mode}>
            <Badge variant={m.mode === BYPASS_MODE ? "high" : "neutral"}>{m.label}</Badge>
            <span className="session-summary__count">{`${plural(m.messages, "reply", "replies")} in ${formatSpan(m.spanNs)}`}</span>
          </li>
        ))}
      </ul>
    </SummarySection>
  );
}

// SummaryTimeline shows when the session was busy: one strip of the commands it started, one of the file, network and DNS events, each
// over the session's span. Each strip is one hue; its accessible label carries the numbers.
function SummaryTimeline({ digest, lastNs }: { readonly digest: AgentSessionDigest; readonly lastNs: number | undefined }) {
  const strips = [
    { label: "Commands started", counts: digest.command_timeline },
    { label: "File, network and DNS events", counts: digest.timeline },
  ].map((s) => ({ ...s, heights: barHeights(s.counts) })).filter((s) => s.heights.length > 0);
  if (strips.length === 0) return <SummarySection title="Timeline"><p>Nothing was recorded over time.</p></SummarySection>;
  const endNs = lastNs ?? digest.timeline_start_ns + digest.timeline_bucket_ns * digest.timeline.length;
  return (
    <SummarySection title="Timeline">
      {strips.map((s) => (
        <div key={s.label} className="session-summary__strip">
          <h3 className="session-summary__subtitle">{s.label}</h3>
          <div
            className="session-summary__bars"
            role="img"
            aria-label={`${s.label}: ${s.counts.reduce((a, b) => a + b, 0).toLocaleString()} across the session, busiest bucket `
              + Math.max(...s.counts).toLocaleString()}
          >
            {s.heights.map((h, i) => (
              // A bucket is a position on the strip: its index is its identity.
              <span key={i} className="session-summary__bar" style={{ height: `${String(h)}%` }} />
            ))}
          </div>
        </div>
      ))}
      <p className="session-summary__axis">
        <span>{formatNs(digest.timeline_start_ns)}</span>
        <span>{formatNs(endNs)}</span>
      </p>
    </SummarySection>
  );
}
