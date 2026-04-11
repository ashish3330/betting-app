import { describe, it, expect } from "vitest";
import { deduplicateMarkets } from "./utils";
import type { Market } from "./api";

function makeMarket(overrides: Partial<Market>): Market {
  return {
    id: "m1",
    name: "Match Odds",
    event_name: "Team A v Team B",
    sport: "cricket",
    status: "OPEN",
    in_play: false,
    start_time: "2026-01-01T00:00:00Z",
    runners: [],
    ...overrides,
  };
}

describe("deduplicateMarkets", () => {
  it("returns an empty array when given no markets", () => {
    expect(deduplicateMarkets([])).toEqual([]);
  });

  it("keeps unrelated markets (different events) separate", () => {
    const a = makeMarket({ id: "evt1-mo", event_id: "evt1", market_type: "match_odds" });
    const b = makeMarket({ id: "evt2-mo", event_id: "evt2", market_type: "match_odds" });
    const out = deduplicateMarkets([a, b]);
    expect(out).toHaveLength(2);
    expect(out.map((m) => m.id).sort()).toEqual(["evt1-mo", "evt2-mo"]);
  });

  it("collapses multiple markets of the same event into one", () => {
    const mo = makeMarket({
      id: "evt1-mo",
      event_id: "evt1",
      market_type: "match_odds",
      total_matched: 100,
    });
    const bm = makeMarket({
      id: "evt1-bm",
      event_id: "evt1",
      market_type: "bookmaker",
      total_matched: 50,
    });
    const out = deduplicateMarkets([bm, mo]);
    expect(out).toHaveLength(1);
    // Prefers match_odds as the surviving entry.
    expect(out[0].market_type).toBe("match_odds");
    // Accumulates total_matched from both.
    expect(out[0].total_matched).toBe(150);
  });

  it("accumulates total_matched when neither market is match_odds", () => {
    const bm = makeMarket({
      id: "evt1-bm",
      event_id: "evt1",
      market_type: "bookmaker",
      total_matched: 80,
    });
    const fancy = makeMarket({
      id: "evt1-fancy1",
      event_id: "evt1",
      market_type: "fancy",
      total_matched: 20,
    });
    const out = deduplicateMarkets([bm, fancy]);
    expect(out).toHaveLength(1);
    expect(out[0].total_matched).toBe(100);
  });

  it("falls back to deriving the event key from id suffix when event_id is missing", () => {
    const mo = makeMarket({ id: "race-7-mo", market_type: "match_odds" });
    const fancy = makeMarket({ id: "race-7-fancy1", market_type: "fancy" });
    const out = deduplicateMarkets([mo, fancy]);
    expect(out).toHaveLength(1);
    expect(out[0].market_type).toBe("match_odds");
  });

  it("does not mutate the input markets", () => {
    const mo = makeMarket({
      id: "evt1-mo",
      event_id: "evt1",
      market_type: "match_odds",
      total_matched: 10,
    });
    const bm = makeMarket({
      id: "evt1-bm",
      event_id: "evt1",
      market_type: "bookmaker",
      total_matched: 5,
    });
    const snapshot = JSON.stringify([mo, bm]);
    deduplicateMarkets([mo, bm]);
    expect(JSON.stringify([mo, bm])).toBe(snapshot);
  });
});
