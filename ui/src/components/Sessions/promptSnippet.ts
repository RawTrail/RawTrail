// SNIPPET_CONTEXT is how many characters of a prompt a result shows on each side of the match.
const SNIPPET_CONTEXT = 80;

// Snippet is a prompt cut to the text around its first match, the match apart so it can be marked.
export interface Snippet {
  before: string;
  match: string;
  after: string;
}

// snippetAround is the first match of query in text, regardless of case, with up to SNIPPET_CONTEXT characters on each side and an
// ellipsis where the prompt goes on. A prompt the server matched by its lowercased text always holds the query.
export function snippetAround(text: string, query: string): Snippet {
  const at = text.toLowerCase().indexOf(query.toLowerCase());
  if (at < 0) return { before: text.slice(0, 2 * SNIPPET_CONTEXT), match: "", after: "" };
  const start = Math.max(0, at - SNIPPET_CONTEXT);
  const end = Math.min(text.length, at + query.length + SNIPPET_CONTEXT);
  return {
    before: (start > 0 ? "..." : "") + text.slice(start, at),
    match: text.slice(at, at + query.length),
    after: text.slice(at + query.length, end) + (end < text.length ? "..." : ""),
  };
}
