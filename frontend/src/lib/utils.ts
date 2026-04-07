import { Market } from "./api";

/**
 * Deduplicate markets: one entry per event.
 * Prefers match_odds over bookmaker/fancy/session.
 * Accumulates total_matched across market types.
 */
export function deduplicateMarkets(markets: Market[]): Market[] {
  const seen = new Map<string, Market>();
  for (const m of markets) {
    const eventKey = m.event_id || m.id.replace(/-mo$|-bm$|-fancy\d*$|-ou$/, "");
    const existing = seen.get(eventKey);
    if (!existing) {
      seen.set(eventKey, { ...m });
    } else {
      if (m.market_type === "match_odds" && existing.market_type !== "match_odds") {
        const prevMatched = existing.total_matched || 0;
        seen.set(eventKey, { ...m, total_matched: (m.total_matched || 0) + prevMatched });
      } else if (existing.total_matched !== undefined && m.total_matched) {
        existing.total_matched = (existing.total_matched || 0) + (m.total_matched || 0);
      }
    }
  }
  return Array.from(seen.values());
}
