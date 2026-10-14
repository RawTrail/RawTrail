import { useMemo } from "react";
import { useSearchParams } from "react-router";

// UrlFilter is a list page's filter state held in the URL: the whitelisted params present, as a plain key->value record; its stable
// serialization, which keys useCursorList so any change resets and refetches; and the updater a FilterChips onChange calls.
export interface UrlFilter {
  activeFilter: Record<string, string>;
  filterKey: string;
  setFilter: (key: string, value: string) => void;
}

// useUrlFilter reads and writes the filter params named by keys, so the filter is bookmarkable and a pivot into the page is a link.
// Shared by every keyset-paginated list (the search modes and the sessions page) so they whitelist and update params alike. Setting
// an empty value removes the param; params outside keys are preserved.
export function useUrlFilter(keys: readonly string[]): UrlFilter {
  const [searchParams, setSearchParams] = useSearchParams();
  // Keyed on the joined names rather than the array, so a caller building its key list per render does not refetch every render.
  const joinedKeys = keys.join("\n");

  const activeFilter = useMemo<Record<string, string>>(() => {
    const entries = joinedKeys
      .split("\n")
      .map((key): [string, string] => [key, searchParams.get(key) ?? ""])
      .filter(([, v]) => v !== "");
    return Object.fromEntries(entries);
  }, [searchParams, joinedKeys]);
  const filterKey = useMemo(() => JSON.stringify(activeFilter), [activeFilter]);

  const setFilter = (key: string, value: string) => {
    const next = new URLSearchParams(searchParams);
    if (value) {
      next.set(key, value);
    } else {
      next.delete(key);
    }
    setSearchParams(next);
  };

  return { activeFilter, filterKey, setFilter };
}
