import { useCallback, useState } from "react";
import { Link } from "react-router";
import { searchAgentPrompts, type PromptSearch as PromptSearchQuery } from "../../api";
import type { FoundAgentPrompt } from "../../types";
import { Button } from "../ui/Button";
import { Table } from "../ui/Table";
import { useCursorList, type CursorPage } from "../Search/useCursorList";
import { SearchResultsFrame } from "../Search/SearchResultsFrame";
import { formatNs } from "../Search/format";
import { useHostNames } from "../useHostNames";
import { MILLISECONDS_PER_DAY, NANOSECONDS_PER_MILLISECOND } from "../../constants";
import { sessionPath } from "./sessionPath";
import { snippetAround } from "./promptSnippet";
import "./PromptSearch.scss";

// SEARCH_WINDOWS are the windows a prompt search can read, in days: the server reads at most thirty (issue #1428).
const SEARCH_WINDOWS = [
  { days: 1, label: "Last 24 hours" },
  { days: 7, label: "Last 7 days" },
  { days: 30, label: "Last 30 days" },
];
// DEFAULT_WINDOW_DAYS is the window a search reads unless the operator picks another. MIN_QUERY is the shortest text the server
// searches for.
const DEFAULT_WINDOW_DAYS = 7;
const MIN_QUERY = 3;
// conversationHref opens a prompt's session on its Conversation, where the prompt reads with what the agent did for it.
function conversationHref(p: FoundAgentPrompt): string {
  return `${sessionPath(p.host_id, p.agent_session_id)}?view=conversation`;
}

// PromptSearch finds the prompts people typed to AI agents across every session (issue #1428): the text searched for, regardless of
// case, within a window of at most thirty days, newest first, each with its host, time and a link to its session's conversation.
// The server audits every search with its text; the page shows it only to an operator who may read prompts.
export function PromptSearch() {
  const hostNames = useHostNames();
  const [draft, setDraft] = useState("");
  const [days, setDays] = useState(DEFAULT_WINDOW_DAYS);
  // submitted is the search on screen, its window resolved when it was asked, so "Load more" pages the same window.
  const [submitted, setSubmitted] = useState<PromptSearchQuery | null>(null);
  const submittedKey = submitted ? `${submitted.q}\n${submitted.from}\n${submitted.to}` : "";

  const fetchPage = useCallback(async (cursor: string): Promise<CursorPage<FoundAgentPrompt>> => {
    if (!submitted) return { rows: [], total: -1 };
    const page = await searchAgentPrompts({ ...submitted, cursor: cursor || undefined });
    return { rows: page.prompts, nextCursor: page.next_cursor, total: -1 };
  }, [submitted]);
  const found = useCursorList(submittedKey, fetchPage);

  const query = draft.trim();
  const submit = () => {
    if (query.length < MIN_QUERY) return;
    const toMs = Date.now();
    setSubmitted({
      q: query,
      from: String((toMs - days * MILLISECONDS_PER_DAY) * NANOSECONDS_PER_MILLISECOND),
      to: String(toMs * NANOSECONDS_PER_MILLISECOND),
    });
  };

  return (
    <section className="prompt-search" aria-label="Search prompts">
      <h2 className="prompt-search__title">Search prompts</h2>
      <form
        className="prompt-search__form"
        onSubmit={(e) => { e.preventDefault(); submit(); }}
      >
        <input
          type="search"
          className="field__input prompt-search__input"
          aria-label="Prompt text"
          placeholder="Text people typed to an agent"
          value={draft}
          onChange={(e) => { setDraft(e.target.value); }}
        />
        <select
          className="field__input"
          aria-label="Prompts typed in"
          value={days}
          onChange={(e) => { setDays(Number(e.target.value)); }}
        >
          {SEARCH_WINDOWS.map((w) => <option key={w.days} value={w.days}>{w.label}</option>)}
        </select>
        <Button type="submit" disabled={query.length < MIN_QUERY}>Search</Button>
      </form>
      <p className="prompt-search__note">Every search is recorded in the audit log with its text.</p>
      {submitted && (
        <SearchResultsFrame
          loading={found.loading}
          error={found.error}
          count={found.rows.length}
          total={-1}
          hasMore={found.hasMore}
          onLoadMore={found.loadMore}
          emptyLabel="No prompts match."
        >
          <Table>
            <thead>
              <tr>
                <th>Typed</th>
                <th>Host</th>
                <th>Prompt</th>
              </tr>
            </thead>
            <tbody>
              {found.rows.map((p) => {
                const snippet = snippetAround(p.text, submitted.q);
                return (
                  <tr key={`${p.host_id}/${p.agent_session_id}/${p.prompt_id}/${String(p.at_ns)}`}>
                    <td>
                      <Link to={conversationHref(p)}>{formatNs(p.at_ns)}</Link>
                    </td>
                    <td>{hostNames.get(p.host_id) ?? p.host_id}</td>
                    <td className="prompt-search__snippet">
                      {snippet.before}
                      {snippet.match && <mark>{snippet.match}</mark>}
                      {snippet.after}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        </SearchResultsFrame>
      )}
    </section>
  );
}
