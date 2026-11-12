import { vi } from "vitest";

import * as api from "../../api";
import type {
  ActivityPage, ActivityProcess, ActivityScope, ActivityTotals, DNSActivityRow, FileActivityRow, NetworkActivityRow,
} from "../../types";

// ClassFixture is one class of a fixture: its totals, the rows of its first page, and the cursor of the page after them.
export interface ClassFixture<Row> {
  events: number;
  distinct: number;
  rows: Row[];
  next_cursor?: string;
}

// ActivityFixture is a scope's activity as a test states it: each class's totals and first page, the scope, and its processes.
export interface ActivityFixture {
  files: ClassFixture<FileActivityRow>;
  network: ClassFixture<NetworkActivityRow>;
  dns: ClassFixture<DNSActivityRow>;
  scope: ActivityScope;
  processes: ActivityProcess[];
}

// totalsOf is a fixture's totals as getActivityTotals answers them.
export function totalsOf(f: ActivityFixture): ActivityTotals {
  const totals = (c: ClassFixture<unknown>) => ({ events: c.events, distinct: c.distinct });
  return { files: totals(f.files), network: totals(f.network), dns: totals(f.dns), scope: f.scope };
}

// firstPageOf is a class's first page as getActivityPage answers it, naming the fixture's processes.
export function firstPageOf(f: ActivityFixture, cls: api.ActivityClassName): ActivityPage<unknown> {
  const byClass = new Map<api.ActivityClassName, ClassFixture<unknown>>([["files", f.files], ["network", f.network], ["dns", f.dns]]);
  const c = byClass.get(cls) ?? f.dns;
  return { rows: c.rows, next_cursor: c.next_cursor, processes: f.processes };
}

// mockActivity serves a fixture as the server does: its totals alone, and each class's first page. A later page is empty unless the
// test mocks it.
export function mockActivity(fixture: ActivityFixture | ((scope: api.ActivityScopeQuery) => ActivityFixture)) {
  const of = typeof fixture === "function" ? fixture : () => fixture;
  const totals = vi.spyOn(api, "getActivityTotals").mockImplementation((_h, scope) => Promise.resolve(totalsOf(of(scope))));
  const page = vi.spyOn(api, "getActivityPage").mockImplementation((_h, scope, cls, cursor) =>
    Promise.resolve(cursor === "" ? firstPageOf(of(scope), cls) : { rows: [], processes: [] }));
  return { totals, page };
}
