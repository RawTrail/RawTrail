import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router";
import "./SessionView.scss";
import {
  getAgentSessionLineage, getAgentSessionTotals, getProcessDetail, getSessionCommands, getSessionConversation, getSessionTree, listAgentSessions, listAlerts, searchSessionProcesses,
} from "../../api";
import type {
  AgentSessionLineage, AgentSessionSummary, AgentSessionTotals, AgentToolCall, Alert, ConversationTurn, ProcessNode, SessionCommand,
  SessionCommandsResponse, ToolCallMatch, TreeResponse,
} from "../../types";
import { useCursorList, type CursorPage } from "../Search/useCursorList";
import { PageHeader } from "../ui/PageHeader";
import { Toggle } from "../ui/Toggle";
import { Input } from "../ui/Input";
import { Button } from "../ui/Button";
import { ProcessDetail } from "../ProcessDetail";
import { ActivityTabs } from "../Activity/ActivityTabs";
import { Badge } from "../ui/Badge";
import { EmptyState } from "../ui/Table";
import { severityBadgeVariant, severityRank } from "../ui/severity";
import { formatNs, executableName, plural } from "../Search/format";
import { findNodeByDbId } from "../ProcessTree.helpers";
import { applyLaunchLinks } from "../launchLinks";
import { useHostNames } from "../useHostNames";
import { NANOSECONDS_PER_MILLISECOND } from "../../constants";
import {
  shapeCommand,
  containsHit,
  containsProcess,
  forkSpan,
  lastActivityNs,
  searchCommands,
  searchRows,
  buildProcessForest,
  countOwn,
  summarizeSession,
  type SearchHits,
  type SessionRow,
  type Command,
} from "./sessionTree";
import { commandLine } from "./toolCommand";
import { sessionPath } from "./sessionPath";
import { mergeTreePages } from "./treePages";
import { BYPASS_MODE, formatSpan, formatTokens, usageLine, type UsageLine } from "./sessionUsage";
import { SessionSummary } from "./SessionSummary";
import { credentialReads } from "./credentialRead";

// AUTO_EXPAND_GROUPS is the most commands a session can have and still open with every one expanded. A longer session opens with
// only the commands an alert was raised in expanded, so the operator starts from what was flagged.
const AUTO_EXPAND_GROUPS = 10;
// ROW_CAP is how many processes a list shows before "Show more": a command that ran a long loop opens on its first rows rather than on
// all of them. A row that holds a search hit is shown regardless.
const ROW_CAP = 20;

// SESSION_LENSES are the session's views, in order: the session at a glance; its timeline, the commands it ran, grouped by default under
// the prompt each was run for (issue #1322); every process as one tree; and what it touched.
type SessionLens = "summary" | "timeline" | "processes" | "activity";
const SESSION_LENSES: readonly { lens: SessionLens; label: string }[] = [
  { lens: "summary", label: "Summary" },
  { lens: "timeline", label: "Timeline" },
  { lens: "processes", label: "Processes" },
  { lens: "activity", label: "Activity" },
];

// linkedGrouping reads a link's view as the Timeline it opens: the views the Timeline replaced, Conversation and Commands, open it
// grouped and ungrouped, so a link saved before the merge still lands where it pointed. Any other view names no grouping.
function linkedGrouping(view: string): boolean | undefined {
  if (view === "conversation") return true;
  if (view === "commands") return false;
  return undefined;
}

// REPORTED_LABEL marks what the agent reported about a command, as distinct from what the sensor observed (issue #1333).
const REPORTED_LABEL = "Reported by agent";
const REPORTED_TITLE = "From the agent's session transcript, which the agent and its user can write; not observed by the sensor";

// AGENT_NAMES are the product names the Processes view calls an agent by, from the agent name its session records.
const AGENT_NAMES = new Map([["claude-code", "Claude Code"]]);

// agentLabel is the name the Processes view calls a session's agent by: its product name, or the agent name it records, or "the
// agent" when it records none.
function agentLabel(agentName: string | undefined): string {
  return (agentName ? AGENT_NAMES.get(agentName) : undefined) ?? agentName ?? "the agent";
}

// internalHelp explains the internal processes toggle (issue #1361): what the agent ran for itself, outside any command.
function internalHelp(agent: string): string {
  return `Plugin scans, git checks, keychain reads and shell snapshots ${agent} ran for itself, outside any command.`;
}

// INTERNAL_TITLE says what an internal process is (issue #1361): one the agent ran for itself, outside any command.
const INTERNAL_TITLE = "The agent ran this for itself, outside any command. It carries no session marker.";

// REPARENTED_TITLE explains a reparented badge: the server marks a process launchd adopted (issue #1280), and the views list it
// under the process that created it, not under launchd.
const REPARENTED_TITLE = "launchd (pid 1) adopted it after the process that created it exited, so its parent is now pid 1; it is " +
  "listed under the process that created it";

// SERVICE_TITLE explains a service badge: launchd started the row for the app it is listed under, which the row names as responsible
// (issue #1340), so the link is the OS's own record, not an inference.
const SERVICE_TITLE = "launchd started it for the app it is listed under, which macOS records as responsible for it";

// INFERRED_LAUNCH_TITLE explains an inferred launch's badge: the row is listed under its launcher, which is not its creator.
const INFERRED_LAUNCH_TITLE =
  "Started by launchd moments after the process it is listed under asked for it: linked by timing and target, not lineage";
// COMMANDS_PAGE is how many commands one read lists; the rest are a "Load more" away (issue #1425).
const COMMANDS_PAGE = 50;
// SEARCH_DELAY_MS is how long the view waits after the last keystroke before it asks the server for the commands a search finds.
const SEARCH_DELAY_MS = 300;

// MAX_SESSION_ALERTS bounds the session's alert read. One more is asked for, so a session with more is reported rather than shown as
// if its first page were all of them.
const MAX_SESSION_ALERTS = 1000;

// SessionRead is everything the view reads for one session, kept with the session it was for so another session's results are never
// shown. The tree is required; the record and the alerts decorate it, and their failures are reported without hiding the tree.
interface SessionRead {
  key: string;
  tree?: TreeResponse;
  error?: string;
  record?: AgentSessionSummary;
  alerts?: Alert[];
  alertsFailed?: boolean;
  // alertsTruncated is set when the session has more alerts than were read, so some are not badged.
  alertsTruncated?: boolean;
  // totals are the session's totals the server counted (issue #1422). Unread, the header counts what the tree read holds.
  totals?: AgentSessionTotals;
  // lineage is the session this one was forked from and the sessions forked from it (issue #1347). Unread, none are linked.
  lineage?: AgentSessionLineage;
}

// SessionRowsContext carries what every row of the view needs: the session's alerts by process, the current search's hits, and the
// selected process, so the nested lists do not thread them through every level.
interface SessionRows {
  hostId: string;
  alertsByProcess: ReadonlyMap<number, Alert[]>;
  hits: SearchHits;
  selectedId: number | null;
  onSelect: (node: ProcessNode) => void;
  // wholeState is how far the read of a large command's whole list has got, and readWhole starts it (issue #1443).
  wholeState: (commandKey: string) => "loading" | "failed" | undefined;
  readWhole: (commandKey: string) => void;
}

const SessionRowsContext = createContext<SessionRows | null>(null);

function useSessionRows(): SessionRows {
  const rows = useContext(SessionRowsContext);
  if (rows === null) throw new Error("useSessionRows outside SessionView");
  return rows;
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : "Unknown error";
}

async function readSession(key: string, hostId: string, sessionId: string): Promise<SessionRead> {
  const [tree, record, alerts, totals, lineage] = await Promise.all([
    getSessionTree(hostId, sessionId).catch((err: unknown) => message(err)),
    listAgentSessions({ host_id: hostId, session: sessionId })
      .then((res) => res.rows[0] as AgentSessionSummary | undefined)
      .catch(() => undefined),
    listAlerts({ host_id: hostId, agent_session: sessionId, limit: MAX_SESSION_ALERTS + 1 }).catch(() => null),
    getAgentSessionTotals(hostId, sessionId).catch(() => undefined),
    getAgentSessionLineage(hostId, sessionId).catch(() => undefined),
  ]);
  // A failed tree read costs the Processes view only: the commands are read on their own, and still carry the session's alerts.
  const read = typeof tree === "string" ? { error: tree } : { tree };
  // The session's alerts are those on its stamped processes; the agent's own processes (issue #1361) carry no session marker, so their
  // alerts are read by the agent that ran them, to badge them in the Processes view.
  const ownAlerts = await Promise.all((read.tree?.agents ?? []).filter((agent) => (agent.children ?? []).length > 0).map((agent) =>
    listAlerts({ host_id: hostId, agent_process: agent.id, served_session: sessionId, limit: MAX_SESSION_ALERTS + 1 }).catch(() => null)));
  const lists = [alerts, ...ownAlerts];
  return {
    key,
    ...read,
    record,
    alerts: lists.flatMap((list) => (list ?? []).slice(0, MAX_SESSION_ALERTS)),
    alertsFailed: lists.includes(null),
    alertsTruncated: lists.some((list) => (list?.length ?? 0) > MAX_SESSION_ALERTS),
    totals,
    lineage,
  };
}

// ListedCommand is a command as a page lists it, with the rows of it that page names as moved under a launcher.
interface ListedCommand extends SessionCommand {
  inferred: number[];
  services: number[];
}

// listedCommands are a commands page's commands, each with the rows the page names as moved under a launcher that are its own.
function listedCommands(page: SessionCommandsResponse): ListedCommand[] {
  const within = (ids: number[], command: SessionCommand) => {
    const own = new Set(command.process_ids);
    return ids.filter((id) => own.has(id));
  };
  return page.commands.map((c) => ({ ...c, inferred: within(page.inferred, c), services: within(page.services, c) }));
}

// WholeCommands are the large commands the operator asked to see whole (issue #1443), read one at a time, for the commands list they
// were asked in: each read, loading, or failed.
interface WholeCommands {
  key: string;
  read: ReadonlyMap<string, ListedCommand | "loading" | "failed">;
}

// CommandsMeta is what a commands page says about the whole read: whether the server's process read, or its tool-call read, was cut.
interface CommandsMeta {
  key: string;
  truncated: boolean;
  toolCallsTruncated: boolean;
}

// useDebounced is value once it has held still for delayMs.
function useDebounced(value: string, delayMs: number): string {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => { setSettled(value); }, delayMs);
    return () => { clearTimeout(timer); };
  }, [value, delayMs]);
  return settled;
}

// useTimelineGrouping is whether the Timeline lists the commands under their prompts: as the operator chose, kept from session to session
// as the plumbing toggle is; as a link's view asked; or else by default when the session recorded a prompt, so a session with none
// opens on its commands. prompts is undefined until the session is read, and so is the default: the Timeline waits rather than switch
// under the operator.
function useTimelineGrouping(linkedView: string, prompts: number | undefined): [boolean | undefined, (grouped: boolean) => void] {
  const [chosen, setChosen] = useState<boolean | null>(null);
  return [chosen ?? linkedGrouping(linkedView) ?? (prompts === undefined ? undefined : prompts > 0), setChosen];
}

// SessionView answers "what did this agent session do" (issue #1284): the session's processes in the order its commands ran, one
// group per command the agent ran, and under it everything that command started, daemons included. The shell plumbing that runs a
// command is hidden unless the operator turns on verbose. Alerts and privilege changes are badged where they happened, and a command
// an alert was raised in opens expanded. A group is what the sensor observed; the tool call the agent reported for it is labelled as
// reported (issue #1333).
export function SessionView() {
  const { hostId = "", sessionId = "" } = useParams();
  const hostNames = useHostNames();
  const [verbose, setVerbose] = useState(false);
  // showOwn lists the processes the agent ran for itself in the Processes view (issue #1361); off by default, so the session's own
  // work reads first, and their count says how many are one click away.
  const [showOwn, setShowOwn] = useState(false);
  // A link may name the lens to open on and a search to open with, as a prompt search's result opens the Timeline grouped by prompt,
  // filtered to the turns holding what was searched for, wherever in the session they are (issue #1428).
  const [searchParams] = useSearchParams();
  const [query, setQuery] = useState(() => searchParams.get("q") ?? "");
  const [selected, setSelected] = useState<{ key: string; node: ProcessNode } | null>(null);
  const readKey = `${hostId}/${sessionId}`;
  // view picks what the session's main column shows: its summary (issue #1429), its timeline of commands, by prompt (issue #1322) or
  // not, every process it ran as one tree (issue #1335), or the totals of what it touched (issue #1285). A session opens on its summary,
  // including one reached from another session's lineage, so the lens chosen is kept for the session it was chosen in.
  const [chosenLens, setChosenLens] = useState<{ key: string; lens: SessionLens } | null>(null);
  const linkedView = searchParams.get("view") ?? "";
  const linkedLens = linkedGrouping(linkedView) !== undefined ? "timeline"
    : SESSION_LENSES.find(({ lens }) => lens === linkedView)?.lens ?? "summary";
  const view: SessionLens = chosenLens?.key === readKey ? chosenLens.lens : linkedLens;
  const setView = useCallback((lens: SessionLens) => { setChosenLens({ key: readKey, lens }); }, [readKey]);
  const [read, setRead] = useState<SessionRead | null>(null);
  const current = read?.key === readKey ? read : null;
  const [grouped, setGrouped] = useTimelineGrouping(linkedView, current === null ? undefined : current.totals?.digest?.prompts ?? 1);
  const tree = current?.tree ?? null;

  useEffect(() => {
    let cancelled = false;
    void readSession(readKey, hostId, sessionId).then((res) => { if (!cancelled) setRead(res); });
    return () => { cancelled = true; };
  }, [hostId, sessionId, readKey]);

  // The process tree's next page, read on request and joined to the pages before it (issue #1425), for the session it was asked for.
  const [moreProcesses, setMoreProcesses] = useState<{ key: string; loading: boolean; failed: boolean } | null>(null);
  const processesMore = moreProcesses?.key === readKey ? moreProcesses : null;
  const loadMoreProcesses = useCallback(() => {
    const cursor = tree?.next_cursor;
    if (!cursor) return;
    setMoreProcesses({ key: readKey, loading: true, failed: false });
    getSessionTree(hostId, sessionId, undefined, cursor)
      .then((page) => {
        setRead((r) => (r?.key === readKey && r.tree ? { ...r, tree: mergeTreePages(r.tree, page) } : r));
        // A page of a session the operator has since left does not settle the state of the one now shown.
        setMoreProcesses((m) => (m?.key === readKey ? { key: readKey, loading: false, failed: false } : m));
      })
      .catch(() => { setMoreProcesses((m) => (m?.key === readKey ? { key: readKey, loading: false, failed: true } : m)); });
  }, [tree, hostId, sessionId, readKey]);

  // The session's commands, a page at a time, as the server groups them (issue #1425). A search asks the server for the commands it
  // finds once the operator stops typing.
  const searched = useDebounced(query.trim(), SEARCH_DELAY_MS);
  const commandsKey = `${readKey}\n${searched}`;
  const [commandsMeta, setCommandsMeta] = useState<CommandsMeta | null>(null);
  // The commands key on screen, read when a page lands so a slower page for a key since replaced cannot overwrite the meta of the list
  // that replaced it.
  const activeCommandsKey = useRef(commandsKey);
  useEffect(() => { activeCommandsKey.current = commandsKey; }, [commandsKey]);
  const fetchCommands = useCallback(async (cursor: string): Promise<CursorPage<ListedCommand>> => {
    const page = await getSessionCommands(hostId, sessionId, { cursor, q: searched, limit: COMMANDS_PAGE });
    if (activeCommandsKey.current === commandsKey) {
      setCommandsMeta({ key: commandsKey, truncated: page.truncated, toolCallsTruncated: page.tool_calls_truncated });
    }
    return { rows: listedCommands(page), nextCursor: page.next_cursor, total: page.total_matched };
  }, [hostId, sessionId, searched, commandsKey]);
  const commands = useCursorList(commandsKey, fetchCommands);
  const meta = commandsMeta?.key === commandsKey ? commandsMeta : null;
  // A large command lists only part of its processes; the operator reads the rest of one on request.
  const [wholeCommands, setWholeCommands] = useState<WholeCommands | null>(null);
  const whole = wholeCommands?.key === commandsKey ? wholeCommands.read : null;
  const readWhole = useCallback((commandKey: string) => {
    const mark = (state: ListedCommand | "loading" | "failed") => {
      setWholeCommands((w) => {
        const read = new Map(w?.key === commandsKey ? w.read : []);
        read.set(commandKey, state);
        return { key: commandsKey, read };
      });
    };
    mark("loading");
    // A read that lands after the operator moved to another session or search is that list's, not the one on screen.
    const landed = (state: ListedCommand | "failed") => { if (activeCommandsKey.current === commandsKey) mark(state); };
    getSessionCommands(hostId, sessionId, { command: commandKey })
      .then((page) => { landed(listedCommands(page).find((c) => c.key === commandKey) ?? "failed"); })
      .catch(() => { landed("failed"); });
  }, [hostId, sessionId, commandsKey]);

  const alertsByProcess = useMemo(() => {
    const byProcess = new Map<number, Alert[]>();
    for (const alert of current?.alerts ?? []) byProcess.set(alert.process_id, [...(byProcess.get(alert.process_id) ?? []), alert]);
    return byProcess;
  }, [current?.alerts]);
  // What launchd started for a command's open or launchctl is listed under it, marked inferred (issue #1286).
  const logical = useMemo(() => (tree ? applyLaunchLinks(tree.roots, tree.links) : null), [tree]);
  // The session's read is incomplete while its record or alerts are unread or cut, or retention removed its early processes. Folding
  // is then off in every view, since an identical-looking row may hide what the view never saw.
  const sessionIncomplete = current === null || current.alertsFailed === true || current.alertsTruncated === true
    || beganBeforeRetention(current);
  const groups = useMemo(
    () => {
      const incomplete = meta?.truncated === true || sessionIncomplete;
      const keep = new Set(alertsByProcess.keys());
      return commands.rows.map((listed) => {
        const read = whole?.get(listed.key);
        const c = typeof read === "object" ? read : listed;
        return shapeCommand(c, { verbose, keep, incomplete, inferred: new Set(c.inferred), services: new Set(c.services) });
      });
    },
    [commands.rows, whole, meta, verbose, alertsByProcess, sessionIncomplete],
  );
  const forest = useMemo(
    () => (tree && logical
      ? buildProcessForest(logical.roots, tree.agents ?? [], { verbose: true, inferred: logical.inferred, services: logical.services },
        showOwn)
      : []),
    [tree, logical, showOwn],
  );
  const summary = useMemo(() => summarizeSession(tree?.roots ?? []), [tree]);
  const hits = useMemo(
    () => (view === "processes" ? searchRows(forest, query) : searchCommands(groups, query)),
    [view, forest, groups, query],
  );
  // A selection belongs to the session it was made in.
  // selection counts the selections made and closed, so a process read for a selection the operator has since replaced or closed is
  // dropped rather than opened over it.
  const selection = useRef(0);
  // A selection belongs to the session it was made in. It needs no tree: a search result or an Activity row opens one without it.
  const selectedNode = selected?.key === readKey ? selected.node : null;
  const rowsContext = useMemo<SessionRows>(
    () => ({
      hostId,
      alertsByProcess,
      hits,
      selectedId: selectedNode?.id ?? null,
      onSelect: (node: ProcessNode) => {
        selection.current += 1;
        setSelected({ key: readKey, node });
      },
      wholeState: (commandKey: string) => {
        const read = whole?.get(commandKey);
        return typeof read === "string" ? read : undefined;
      },
      readWhole,
    }),
    [hostId, alertsByProcess, hits, selectedNode, readKey, whole, readWhole],
  );

  const record = current?.record;
  const agentName = record?.agent_name ?? summary.agentName;
  const agentVersion = record?.agent_version ?? summary.agentVersion;
  const title = agentName ? `${agentName}${agentVersion ? ` ${agentVersion}` : ""}` : "Agent session";
  const firstNs = record?.first_seen_ns ?? summary.firstNs;
  const lastNs = Math.max(record?.last_seen_ns ?? 0, summary.lastNs ?? 0) || undefined;
  const project = record?.cwd ?? summary.project;
  const subtitle = (
    <span className="session-view__facts">
      {project && <span className="session-view__mono">{project}</span>}
      <span>{hostNames.get(hostId) ?? hostId}</span>
      {firstNs !== undefined && lastNs !== undefined && <span>{`${formatNs(firstNs)} to ${formatNs(lastNs)}`}</span>}
      {current?.totals && <span>{plural(current.totals.commands, "command", "commands")}</span>}
      {(current?.totals ?? tree) && (
        <span>{plural(current?.totals?.processes ?? summary.processCount, "process", "processes")}</span>
      )}
      {record?.agent_pid !== undefined && <span>agent pid {record.agent_pid}</span>}
      <span className="session-view__mono" title={sessionId}>Session {sessionId}</span>
      <SessionUsage usage={usageLine(current?.totals?.usage)} />
    </span>
  );

  return (
    <div className="session-view">
      <Link to="/agent-sessions" className="session-view__back">All agent sessions</Link>
      <PageHeader
        title={title}
        subtitle={subtitle}
        actions={(
          <div className="session-view__actions">
            {/* The Summary has nothing to search; the box keeps its space so the header does not move between views. */}
            <span style={view === "summary" ? { visibility: "hidden" } : undefined}>
              <Input
                type="search"
                aria-label="Search this session"
                placeholder={`Search ${view}`}
                value={query}
                onChange={(e) => { setQuery(e.target.value); }}
              />
            </span>
            {/* The Timeline's toggles belong to it alone: Processes lists every process already. They keep their space in every view,
                hidden rather than removed, so the header wraps alike in all of them and the view switcher below never moves. */}
            <span style={view === "timeline" ? undefined : { visibility: "hidden" }}>
              <Toggle
                id="session-grouped"
                label="Group by prompt"
                checked={grouped === true}
                onChange={(e) => { setGrouped(e.target.checked); }}
              />
            </span>
            <span style={view === "timeline" ? undefined : { visibility: "hidden" }}>
              <Toggle
                id="session-verbose"
                label="Show shell plumbing"
                checked={verbose}
                onChange={(e) => { setVerbose(e.target.checked); }}
              />
            </span>
          </div>
        )}
      />
      <SessionLineageLine hostId={hostId} lineage={current?.lineage} />
      <SessionNotices read={current} meta={meta} commandsShown={view === "timeline" && grouped === false} view={view} />
      <SessionRowsContext.Provider value={rowsContext}>
        <div className="session-view__layout">
          <div className="session-view__main">
            <div role="group" aria-label="Session view" className="session-view__views">
              {SESSION_LENSES.map(({ lens: v, label }) => (
                <button
                  key={v}
                  type="button"
                  aria-pressed={view === v}
                  className="session-view__view"
                  onClick={() => { setView(v); }}
                >
                  {label}
                </button>
              ))}
            </div>
            {view === "summary" && !current && <EmptyState>Loading...</EmptyState>}
            {view === "summary" && current && (
              <SessionSummary
                hostId={hostId}
                alerts={current.alerts ?? []}
                totals={current.totals}
                agent={title}
                project={project}
                firstNs={firstNs}
                lastNs={lastNs}
                onAlerts={() => { setQuery(""); setGrouped(false); setView("timeline"); }}
              />
            )}
            {view === "timeline" && grouped === undefined && <EmptyState>Loading...</EmptyState>}
            {view === "timeline" && grouped === true && (
              <ConversationBody hostId={hostId} sessionId={sessionId} query={searched} incomplete={sessionIncomplete} verbose={verbose} />
            )}
            {view === "timeline" && grouped === false && (
              <SessionBody
                groups={groups}
                commands={commands}
                expandAll={(current?.totals?.commands ?? commands.total) <= AUTO_EXPAND_GROUPS}
              />
            )}
            {view === "processes" && searched !== "" && (
              <ProcessSearchBody hostId={hostId} sessionId={sessionId} query={searched} />
            )}
            {view === "processes" && searched === "" && (
              <ProcessesBody
                read={current}
                forest={forest}
                agent={agentLabel(agentName)}
                showOwn={showOwn}
                onShowOwn={setShowOwn}
                more={processesMore}
                onMore={loadMoreProcesses}
              />
            )}
            {view === "activity" && (
              <ActivityTabs
                label="Session activity"
                hostId={hostId}
                scope={{ kind: "session", session: sessionId }}
                search={searched}
                onProcess={(process, atNs) => {
                  // The agent is not among the roots, and its own edits are what the Files tab lists most (issue #1287).
                  const node = tree ? findNodeByDbId([...(tree.agents ?? []), ...tree.roots], process.id) : null;
                  if (node) {
                    rowsContext.onSelect(node);
                    return;
                  }
                  // A row can name a process on a tree page not yet read, which a search reaches (issue #1452): read that process.
                  const asked = selection.current;
                  void getProcessDetail(hostId, process.pid, atNs, process.pidversion)
                    .then((detail) => { if (selection.current === asked) rowsContext.onSelect(detail.process); })
                    .catch(() => undefined);
                }}
              />
            )}
          </div>
          {selectedNode && (
            <aside className="session-view__detail">
              <Link className="session-view__graph-link" to={hostHref(hostId, selectedNode.id, selectedNode.fork_time_ns)}>
                Open in host graph
              </Link>
              <ProcessDetail
                key={selectedNode.id}
                hostId={hostId}
                node={selectedNode}
                onClose={() => {
                  selection.current += 1;
                  setSelected(null);
                }}
                timelineHref={timelineHref(hostId, selectedNode)}
              />
            </aside>
          )}
        </div>
      </SessionRowsContext.Provider>
    </div>
  );
}

// SessionUsage says what the session's messages used (issue #1427): its models, its tokens, and the permission modes the agent ran in,
// each with how many replies and over how long. Bypass mode, in which the agent runs tools without asking, is flagged.
function SessionUsage({ usage }: { readonly usage: UsageLine | null }) {
  if (!usage) return null;
  // The split is written out, not left to a tooltip a keyboard or a touch screen cannot open.
  const split = `${formatTokens(usage.input)} in, ${formatTokens(usage.cache)} cache, ${formatTokens(usage.output)} out`;
  return (
    <span className="session-view__usage">
      <span>{usage.models.join(", ")}</span>
      <span>{`${formatTokens(usage.total)} tokens (${split})`}</span>
      {usage.modes.map((m) => (
        <span key={m.mode} className="session-view__mode">
          <Badge variant={m.mode === BYPASS_MODE ? "high" : "neutral"}>{m.label}</Badge>
          {`${plural(m.messages, "reply", "replies")} in ${formatSpan(m.spanNs)}`}
        </span>
      ))}
    </span>
  );
}

// SessionLineageLine links a forked session to the session it was forked from, and a session to those forked from it (issue #1347).
// A fork is a session of its own: its commands are its own, and the history it copied from its parent is listed under the parent.
function SessionLineageLine({ hostId, lineage }: { readonly hostId: string; readonly lineage: AgentSessionLineage | undefined }) {
  const parent = lineage?.forked_from_session_id;
  const forks = lineage?.forked_session_ids ?? [];
  if (!parent && forks.length === 0) return null;
  const link = (session: string) => (
    <Link key={session} className="session-view__mono" to={sessionPath(hostId, session)} title={session}>
      {`Session ${session}`}
    </Link>
  );
  return (
    <p className="session-view__lineage">
      {parent && <span>Forked from {link(parent)}</span>}
      {forks.length > 0 && (
        <span>
          {"Forked into "}
          {forks.map((session, i) => <span key={session}>{i > 0 && ", "}{link(session)}</span>)}
        </span>
      )}
    </p>
  );
}

// SessionNotices says what the view is not showing: processes past the read's limit, processes retention removed, and alerts that
// could not be read.
// beganBeforeRetention reports a session first seen before the retention cutoff, so processes it ran that finished before then are
// gone from the read.
function beganBeforeRetention(read: SessionRead): boolean {
  const retainedFrom = read.tree?.retained_from_ns;
  return retainedFrom !== undefined && read.record !== undefined && read.record.first_seen_ns < retainedFrom;
}

// Each read's cut is told with the view it cuts: the commands with Commands, the process tree with Processes.
function SessionNotices({ read, meta, commandsShown, view }: {
  readonly read: SessionRead | null;
  readonly meta: CommandsMeta | null;
  // commandsShown is set while the Timeline lists the commands ungrouped, the list the commands read's cuts belong to.
  readonly commandsShown: boolean;
  readonly view: SessionLens;
}) {
  if (!read) return null;
  const tree = read.tree;
  const retainedFrom = tree?.retained_from_ns;
  return (
    <>
      {view === "processes" && tree?.truncated && (
        <p className="session-view__notice" role="status">
          Showing the first {tree.returned.toLocaleString()} of {tree.total_matched_capped ? "more than " : ""}
          {tree.total_matched.toLocaleString()} processes in this session.
        </p>
      )}
      {retainedFrom !== undefined && beganBeforeRetention(read) && (
        <p className="session-view__notice" role="status">
          This session began before {formatNs(retainedFrom)}, and processes that finished before then have been removed by retention.
        </p>
      )}
      {read.alertsFailed && (
        <p className="session-view__notice" role="status">This session&apos;s alerts could not be loaded, so none are shown.</p>
      )}
      {commandsShown && meta?.truncated && (
        <p className="session-view__notice" role="status">
          This session ran more processes than the server reads, so its later commands are not listed.
        </p>
      )}
      {commandsShown && meta?.toolCallsTruncated && (
        <p className="session-view__notice" role="status">
          The agent reported more tool calls than are read, so some commands may show without the tool call the agent reported for
          them.
        </p>
      )}
      {read.alertsTruncated && (
        <p className="session-view__notice" role="status">
          This session has more than {MAX_SESSION_ALERTS.toLocaleString()} alerts; only the {MAX_SESSION_ALERTS.toLocaleString()} most
          recent are badged.
        </p>
      )}
    </>
  );
}

// SessionBody lists the session's commands a page at a time. A session of at most AUTO_EXPAND_GROUPS commands opens with every one
// expanded; a longer one with only those an alert was raised in.
function SessionBody({ groups, commands, expandAll }: {
  readonly groups: Command[];
  readonly commands: { error: string | null; loading: boolean; hasMore: boolean; total: number; loadMore: () => void };
  readonly expandAll: boolean;
}) {
  const { alertsByProcess } = useSessionRows();
  // A failed later page keeps the commands already listed, and the button that retries it.
  if (commands.error && groups.length === 0) return <EmptyState>Error: {commands.error}</EmptyState>;
  if (groups.length === 0) {
    return <EmptyState>{commands.loading ? "Loading..." : "No processes are recorded for this session."}</EmptyState>;
  }
  return (
    <>
      <ol className="session-view__calls">
        {groups.map((group) => {
          const alerts = group.processIds.flatMap((id) => alertsByProcess.get(id) ?? []);
          return (
            <CommandGroup
              key={group.key}
              group={group}
              match={group.toolCall}
              alerts={alerts}
              defaultOpen={expandAll || alerts.length > 0}
            />
          );
        })}
      </ol>
      {commands.error && (
        <p className="session-view__notice" role="alert">The next commands could not be loaded: {commands.error}</p>
      )}
      {commands.hasMore && (
        <Button variant="text-link" disabled={commands.loading} onClick={commands.loadMore}>
          {`Load more commands (${(commands.total - groups.length).toLocaleString()} more)`}
        </Button>
      )}
    </>
  );
}

// ProcessSearchBody lists the processes a search found across the whole session (issue #1452), each alone and in fork order, from the
// server and a page at a time: the tree a match sits in may lie on a page not yet read, so the loaded tree cannot answer it.
function ProcessSearchBody({ hostId, sessionId, query }: {
  readonly hostId: string;
  readonly sessionId: string;
  readonly query: string;
}) {
  const key = `${hostId}/${sessionId}\n${query}`;
  const [cut, setCut] = useState<{ key: string; truncated: boolean } | null>(null);
  // The key on screen, read when a page lands so a slower page for a search since replaced cannot report its cut as this one's.
  const activeKey = useRef(key);
  useEffect(() => { activeKey.current = key; }, [key]);
  const fetchPage = useCallback(async (cursor: string): Promise<CursorPage<SessionRow>> => {
    const page = await searchSessionProcesses(hostId, sessionId, { cursor, q: query, limit: COMMANDS_PAGE });
    if (activeKey.current === key) setCut({ key, truncated: page.truncated });
    return { rows: page.processes.map((node) => ({ node, children: [] })), nextCursor: page.next_cursor, total: page.total_matched };
  }, [hostId, sessionId, query, key]);
  const found = useCursorList(key, fetchPage);
  if (found.error && found.rows.length === 0) return <EmptyState>Error: {found.error}</EmptyState>;
  const partial = cut?.key === key && cut.truncated;
  const notice = partial && (
    <p className="session-view__notice" role="status">
      This session ran more processes than the server reads, so a match among the later ones is not found.
    </p>
  );
  if (found.rows.length === 0) {
    if (found.loading) return <EmptyState>Searching...</EmptyState>;
    // A cut read cannot say that nothing matches, only that nothing it read does.
    return <>{notice}<EmptyState>{partial ? "No process the server read matches." : "No process in this session matches."}</EmptyState></>;
  }
  return (
    <div className="session-view__processes">
      <p className="session-view__processes-summary">{plural(found.total, "process matches", "processes match")}</p>
      {notice}
      {/* The server pages the matches, so every match read is listed: no second cap behind Load more. */}
      <ProcessRows rows={found.rows} cap={Infinity} />
      {found.error && <p className="session-view__notice" role="alert">The next processes could not be loaded: {found.error}</p>}
      {found.hasMore && (
        <Button variant="text-link" disabled={found.loading} onClick={found.loadMore}>
          {`Load more matches (${(found.total - found.rows.length).toLocaleString()} more)`}
        </Button>
      )}
    </div>
  );
}

// ProcessesBody lists every process the session ran as one tree rooted at the agent (issue #1335), with nothing hidden or folded: the
// forensic counterpart to the commands, which group and trim the same processes for reading.
function ProcessesBody({ read, forest, agent, showOwn, onShowOwn, more, onMore }: {
  readonly read: SessionRead | null;
  readonly forest: SessionRow[];
  // agent is the name the view calls the session's agent by, such as Claude Code.
  readonly agent: string;
  readonly showOwn: boolean;
  readonly onShowOwn: (show: boolean) => void;
  // more is the state of the next page's read; onMore reads it.
  readonly more: { loading: boolean; failed: boolean } | null;
  readonly onMore: () => void;
}) {
  if (read?.error) return <EmptyState>Error: {read.error}</EmptyState>;
  if (!read?.tree) return <EmptyState>Loading...</EmptyState>;
  if (forest.length === 0) return <EmptyState>No processes are recorded for this session.</EmptyState>;
  const own = countOwn(read.tree.agents ?? []);
  return (
    <div className="session-view__processes">
      <p className="session-view__processes-summary">
        {processesSummary(forest, agent, showOwn && read.tree.agent_own_truncated === true)}
      </p>
      {own > 0 && (
        <div className="session-view__own">
          <Toggle
            id="session-own"
            label={`Show internal processes (${own.toLocaleString()}${read.tree.agent_own_truncated ? "+" : ""})`}
            aria-describedby="session-own-help"
            checked={showOwn}
            onChange={(e) => { onShowOwn(e.target.checked); }}
          />
          {/* The switch's description: read with it by a screen reader, and shown beside it on hover or keyboard focus. */}
          <span id="session-own-help" className="session-view__own-help">{internalHelp(agent)}</span>
        </div>
      )}
      <ProcessRows rows={forest} />
      {more?.failed && <p className="session-view__notice" role="status">The next processes could not be loaded.</p>}
      {read.tree.next_cursor && (
        <Button variant="text-link" disabled={more?.loading === true} onClick={onMore}>
          Load more processes
        </Button>
      )}
    </div>
  );
}

// possessive is a name with its possessive ending: "Claude Code's", "the agent's".
function possessive(name: string): string {
  return `${name}'s`;
}

// countListedRows counts the processes a forest lists below its agents, folded ones included: the session's, and the agent's internal
// ones when they are shown. The agent's own row heads the list either way and is not counted.
function countListedRows(rows: readonly SessionRow[]): number {
  return rows.reduce((sum, listed) => sum + (listed.agent ? 0 : 1 + (listed.folded?.length ?? 0)) + countListedRows(listed.children), 0);
}

// processesSummary counts the processes the Processes view lists (issue #1361), with + when the internal ones it shows were cut, and
// says when its roots do not all hang under the agent: some beside it, whose creator was not read, or all, when it was not seen.
function processesSummary(forest: readonly SessionRow[], agent: string, cut: boolean): string {
  const listed = countListedRows(forest);
  const count = cut ? `${listed.toLocaleString()}+ processes` : plural(listed, "process", "processes");
  const beside = forest.filter((r) => !r.agent).length;
  if (beside === forest.length) return `${count}. ${possessive(agent)} process was not seen, so its commands are listed as roots`;
  if (beside === 0) return count;
  return `${count}. ${plural(beside, "root sits", "roots sit")} beside ${agent}: the process that created ${beside === 1 ? "it" : "them"} ` +
    "was not read";
}

// highestSeverity is the most severe of the alerts' severities, for the group's badge.
function highestSeverity(alerts: readonly Alert[]): string {
  return alerts.reduce((top, a) => (severityRank(a.severity) < severityRank(top) ? a.severity : top), "");
}

// ListedTurn is a turn as the Conversation view lists it: its commands shaped as the Commands view shapes them.
interface ListedTurn {
  turn: ConversationTurn;
  groups: Command[];
}

// ConversationMeta is what a conversation page says about the whole read, kept with the key it was read for.
interface ConversationMeta {
  key: string;
  promptsHidden: boolean;
  processesCut: boolean;
  cut: boolean;
}

// ConversationBody lists the session as its turns (issue #1322), a page at a time from the server: each prompt the person typed, the
// commands the agent ran for it and the tool calls it made that ran none, and last what belongs to no prompt. A search asks the
// server, over prompt text the operator may read, commands and calls. The pages are read as the server sends them and shaped here, so
// alerts read later reshape the turns already loaded rather than reading them again.
function ConversationBody({ hostId, sessionId, query, incomplete, verbose }: {
  readonly hostId: string;
  readonly sessionId: string;
  readonly query: string;
  readonly verbose: boolean;
  // incomplete is the session view's own: its record or alerts are unread or cut, or retention removed early processes.
  readonly incomplete: boolean;
}) {
  const rows = useSessionRows();
  const key = `${hostId}/${sessionId}\n${query}`;
  const [meta, setMeta] = useState<ConversationMeta | null>(null);
  // The key on screen, read when a page lands so a slower page for a search since replaced cannot report its flags as this one's.
  const activeKey = useRef(key);
  useEffect(() => { activeKey.current = key; }, [key]);
  const fetchPage = useCallback(async (cursor: string): Promise<CursorPage<ConversationTurn>> => {
    const page = await getSessionConversation(hostId, sessionId, { cursor, q: query, limit: COMMANDS_PAGE });
    if (activeKey.current === key) {
      setMeta({ key, promptsHidden: page.prompts_hidden, processesCut: page.truncated,
        cut: page.truncated || page.tool_calls_truncated || page.prompts_truncated });
    }
    return { rows: page.turns, nextCursor: page.next_cursor, total: page.total_matched };
  }, [hostId, sessionId, query, key]);
  const turns = useCursorList(key, fetchPage);
  const current = meta?.key === key ? meta : null;
  const listed = useMemo<ListedTurn[]>(() => {
    const shape = { verbose, keep: new Set(rows.alertsByProcess.keys()), incomplete: incomplete || current?.processesCut === true };
    return turns.rows.map((turn) => ({ turn, groups: turn.commands.map((c) => shapeCommand(c, shape)) }));
  }, [turns.rows, rows.alertsByProcess, incomplete, current?.processesCut, verbose]);
  // The search's hits are found in these groups, so a matching row is marked and listed past the cap, and its command opens.
  const scoped = useMemo<SessionRows>(
    () => ({ ...rows, hits: searchCommands(listed.flatMap((t) => t.groups), query) }),
    [rows, listed, query],
  );
  if (turns.error && turns.rows.length === 0) return <EmptyState>Error: {turns.error}</EmptyState>;
  const cutNotice = current?.cut === true && (
    <p className="session-view__notice" role="status">
      This session holds more than the server reads, so some of its prompts, commands or tool calls are not listed.
    </p>
  );
  if (turns.rows.length === 0) {
    if (turns.loading) return <EmptyState>Loading...</EmptyState>;
    // A cut read cannot say that nothing matches, only that nothing it read does.
    const none = query ? (current?.cut === true ? "No turn the server read matches." : "No turn of this session matches.")
      : "No prompts are recorded for this session.";
    return <>{cutNotice}<EmptyState>{none}</EmptyState></>;
  }
  return (
    <SessionRowsContext.Provider value={scoped}>
      {cutNotice}
      <ol className="session-view__turns">
        {listed.map(({ turn, groups }) => (
          <TurnEntry key={`${turn.prompt_id ?? ""}/${String(turn.uid ?? "")}`} turn={turn} groups={groups}
            promptsHidden={current?.promptsHidden === true} />
        ))}
      </ol>
      {turns.error && <p className="session-view__notice" role="alert">The next turns could not be loaded: {turns.error}</p>}
      {turns.hasMore && (
        <Button variant="text-link" disabled={turns.loading} onClick={turns.loadMore}>
          {`Load more turns (${(turns.total - turns.rows.length).toLocaleString()} more)`}
        </Button>
      )}
    </SessionRowsContext.Provider>
  );
}

// TurnEntry is one turn: the prompt the person typed, labelled as the agent's report since the transcript is the user's to write,
// then the commands the agent ran for it and the tool calls that ran none.
function TurnEntry({ turn, groups, promptsHidden }: {
  readonly turn: ConversationTurn;
  readonly groups: Command[];
  readonly promptsHidden: boolean;
}) {
  const { alertsByProcess } = useSessionRows();
  return (
    <li className="session-view__turn">
      <div className="session-view__prompt">
        {turn.prompt_id === undefined ? (
          <span className="session-view__prompt-text">Outside any prompt</span>
        ) : (
          <>
            {/* Only a prompt the agent captured is its report; an uncaptured one says so instead. */}
            {turn.prompt && <span className="session-view__reported" title={REPORTED_TITLE}>{REPORTED_LABEL}</span>}
            <span className="session-view__prompt-text">{promptText(turn, promptsHidden)}</span>
          </>
        )}
        <span className="session-view__meta">{formatNs(turn.at_ns)}</span>
      </div>
      {groups.length > 0 && (
        <ol className="session-view__calls">
          {groups.map((group) => {
            const alerts = group.processIds.flatMap((id) => alertsByProcess.get(id) ?? []);
            return <CommandGroup key={group.key} group={group} match={group.toolCall} alerts={alerts} defaultOpen={alerts.length > 0} />;
          })}
        </ol>
      )}
      {turn.calls.length > 0 && (
        <ul className="session-view__turn-calls">
          {turn.calls.map((call) => <TurnCall key={`${call.tool_use_id}/${String(call.uid)}`} call={call} />)}
        </ul>
      )}
    </li>
  );
}

// promptText is what a turn shows for its prompt: the text, or why there is none.
function promptText(turn: ConversationTurn, promptsHidden: boolean): string {
  if (!turn.prompt) return "Prompt not captured";
  if (promptsHidden) return "Prompt hidden: you need permission to read prompts";
  return turn.prompt.text;
}

// TurnCall is a tool call of a turn that ran no command: a file read or edit, a web fetch, an MCP tool.
function TurnCall({ call }: { readonly call: AgentToolCall }) {
  return (
    <li className="session-view__turn-call">
      <code className="session-view__tool-name">{call.tool_name}</code>
      <span className="session-view__mono">{call.file_path ?? call.command ?? ""}</span>
      <span className="session-view__meta">{formatNs(call.called_at_ns)}</span>
    </li>
  );
}

// CommandGroup is one command. A search that finds something in it opens it, whatever the operator last chose.
function CommandGroup({ group, match, alerts, defaultOpen }: {
  readonly group: Command;
  readonly match: ToolCallMatch | undefined;
  readonly alerts: readonly Alert[];
  readonly defaultOpen: boolean;
}) {
  const { hits, wholeState, readWhole } = useSessionRows();
  const wholeRead = wholeState(group.key);
  // The group follows defaultOpen until the operator opens or closes it, so alerts read after the commands still open their groups.
  const [toggled, setToggled] = useState<boolean | null>(null);
  const found = hits.byGroup.get(group.key) ?? 0;
  const expanded = (toggled ?? defaultOpen) || found > 0;
  // A credential the command read, or was stopped from reading, is said in words beside it, open or closed (issue #1432).
  const reads = credentialReads(alerts);
  return (
    <li className="session-view__call">
      <button
        type="button"
        className="session-view__call-header"
        aria-expanded={expanded}
        onClick={() => { setToggled(!expanded); }}
      >
        <code className="session-view__command">{group.command}</code>
        <span className="session-view__meta">
          {formatNs(group.startNs)} · {plural(group.processCount, "process", "processes")}
        </span>
        {alerts.length > 0 && (
          <Badge variant={severityBadgeVariant(highestSeverity(alerts))}>{plural(alerts.length, "alert", "alerts")}</Badge>
        )}
        {found > 0 && <span className="session-view__found">{plural(found, "match", "matches")}</span>}
        {match && <AgentCall match={match} />}
      </button>
      {reads.length > 0 && (
        <ul className="session-view__credential-reads">
          {reads.map((r) => (
            <li key={r.alertId}>
              <Link to={`/alerts/${String(r.alertId)}`}>
                <Badge variant={r.stopped ? "info" : "high"}>{r.text}</Badge>
              </Link>
              {/* The rule's author is credited wherever its alert is shown (issue #765), here as on the process row. */}
              {r.origin && <span className="session-view__meta">{r.origin}</span>}
            </li>
          ))}
        </ul>
      )}
      {expanded && group.rows.length > 0 && <ProcessRows rows={group.rows} />}
      {expanded && group.processesCut && (
        <p className="session-view__notice" role="status">
          {"Some of this command's processes are not listed. "}
          <Button variant="text-link" disabled={wholeRead === "loading"} onClick={() => { readWhole(group.key); }}>
            {`Show all ${group.processCount.toLocaleString()} processes`}
          </Button>
          {wholeRead === "failed" && " They could not be loaded."}
        </p>
      )}
    </li>
  );
}

// AgentCall names the tool call the agent reported for a group (issue #1288): the description the agent gave it and its tool_use_id,
// or, for identical calls of one turn that cannot be told apart, the calls it is one of. It is labelled as reported (issue #1333):
// it comes from the agent's transcript, a file the agent and its user can write, not from the sensor.
function AgentCall({ match }: { readonly match: ToolCallMatch }) {
  if (match.kind === "ambiguous") {
    const ids = match.candidates.map((c) => c.tool_use_id);
    const unlisted = match.candidate_count - ids.length;
    return (
      <span className="session-view__agent-call" title={`One of: ${ids.join(", ")}${unlisted > 0 ? ` and ${String(unlisted)} more` : ""}`}>
        <span className="session-view__reported" title={REPORTED_TITLE}>{REPORTED_LABEL}</span>
        One of {match.candidate_count} identical calls made at once
      </span>
    );
  }
  return (
    <span className="session-view__agent-call">
      <span className="session-view__reported" title={REPORTED_TITLE}>{REPORTED_LABEL}</span>
      {match.call.description && <span>{match.call.description}</span>}
      <code className="session-view__tool-use-id">{match.call.tool_use_id}</code>
    </span>
  );
}

// ProcessRows lists rows up to ROW_CAP, then offers the rest. A row holding a search hit or an alert is listed past the cap, since
// the command opened, or the search expanded it, to show that row.
function ProcessRows({ rows, cap = ROW_CAP }: { readonly rows: SessionRow[]; readonly cap?: number }) {
  const { hits, alertsByProcess } = useSessionRows();
  const [shown, setShown] = useState(cap);
  const listed = rows.filter((r, i) => i < shown || containsHit(r, hits) || containsProcess(r, alertsByProcess));
  const rest = rows.length - listed.length;
  return (
    <ul className="session-view__rows">
      {listed.map((r) => <ProcessRow key={r.node.id} listed={r} />)}
      {rest > 0 && (
        <li>
          <Button variant="text-link" onClick={() => { setShown(rows.length); }}>
            Show {plural(rest, "more process", "more processes")}
          </Button>
        </li>
      )}
    </ul>
  );
}

function ProcessRow({ listed }: { readonly listed: SessionRow }) {
  const { alertsByProcess, hits, selectedId, onSelect } = useSessionRows();
  const { node, children, uidChange, folded, inferred, service, agent, own } = listed;
  const span = folded ? forkSpan(listed) : null;
  const rowClass = `session-view__row${hits.rows.has(listed) ? " session-view__row--hit" : ""}`;
  return (
    <li>
      <div className={rowClass}>
        <button
          type="button"
          className="session-view__name"
          aria-pressed={selectedId === node.id}
          onClick={() => { onSelect(node); }}
        >
          {executableName(node.path)}
        </button>
        <span className="session-view__mono session-view__cmdline">{commandLine(node.path, node.args)}</span>
        {agent && <Badge variant="info">agent</Badge>}
        {own && (
          <span title={INTERNAL_TITLE}>
            <Badge variant="neutral">internal</Badge>
          </span>
        )}
        {folded && <Badge variant="neutral">{`×${String(folded.length + 1)}`}</Badge>}
        <span className="session-view__meta">
          {span
            ? `${formatNs(span.firstNs)} to ${formatNs(span.lastNs)}`
            : `pid ${String(node.pid)} · ${formatNs(node.fork_time_ns)}`}
          {node.exit_code !== undefined && node.exit_reason === "event" ? ` · exit ${String(node.exit_code)}` : ""}
        </span>
        {inferred && (
          <span title={INFERRED_LAUNCH_TITLE}>
            <Badge variant="info">inferred launch</Badge>
          </span>
        )}
        {service && (
          <span title={SERVICE_TITLE}>
            <Badge variant="info">service</Badge>
          </span>
        )}
        {/* A double fork or nohup left it to launchd (issue #1280); it is still listed under the process that created it. */}
        {node.reparented && (
          <span title={REPARENTED_TITLE}>
            <Badge variant="neutral">reparented, ppid 1</Badge>
          </span>
        )}
        {uidChange && (
          <span title={`Runs as uid ${String(uidChange.to)}; its creator ran as uid ${String(uidChange.from)}`}>
            <Badge variant="high">{uidChange.to === 0 ? "runs as root" : `runs as uid ${String(uidChange.to)}`}</Badge>
          </span>
        )}
        {(alertsByProcess.get(node.id) ?? []).map((alert) => (
          <span key={alert.id} className="session-view__alert">
            <Link to={`/alerts/${String(alert.id)}`}>
              <Badge variant={severityBadgeVariant(alert.severity)}>{alert.title}</Badge>
            </Link>
            {/* A vendored rule's match owes its author the credit on every surface that shows it (issue #765). */}
            {alert.origin && <span className="session-view__meta">{alert.origin}</span>}
          </span>
        ))}
      </div>
      {children.length > 0 && <ProcessRows rows={children} />}
    </li>
  );
}

// timelineHref opens a process's events in its host's timeline, on a window ending just after its last observed activity, so its exec
// and anything it did after are inside the window.
function timelineHref(hostId: string, node: ProcessNode): string {
  const atMs = Math.floor(lastActivityNs(node) / NANOSECONDS_PER_MILLISECOND) + 1;
  return `/hosts/${encodeURIComponent(hostId)}?view=timeline&pid=${String(node.pid)}&at=${String(atMs)}`;
}

// hostHref opens a process in its host's graph, selected and anchored at its fork time: the graph's ?process= takes the row id.
function hostHref(hostId: string, processId: number, forkNs: number): string {
  const atMs = Math.floor(forkNs / NANOSECONDS_PER_MILLISECOND) + 1;
  return `/hosts/${encodeURIComponent(hostId)}?process=${String(processId)}&at=${String(atMs)}`;
}
