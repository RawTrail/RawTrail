// MIN_VISIBLE_BAR_PERCENT keeps a tiny-but-nonzero bucket visible: a 1-count bucket next to a 400-count spike must still read as a
// mark, not vanish into the baseline.
const MIN_VISIBLE_BAR_PERCENT = 6;
const FULL_PERCENT = 100;

// barHeight is a bucket's bar height in percent of the busiest bucket's count max: zero for an empty bucket, at least
// MIN_VISIBLE_BAR_PERCENT for any other. The host page's activity strip and a session Summary's timeline share it.
export function barHeight(count: number, max: number): number {
  return count === 0 ? 0 : Math.max(MIN_VISIBLE_BAR_PERCENT, (count / max) * FULL_PERCENT);
}
