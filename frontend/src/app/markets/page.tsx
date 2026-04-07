"use client";

import { useEffect, useState } from "react";
import { api, Market } from "@/lib/api";
import MarketCard from "@/components/MarketCard";

type Filter = "all" | "live" | "upcoming";

export default function MarketsPage() {
  const [markets, setMarkets] = useState<Market[]>([]);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState<Filter>("all");

  useEffect(() => {
    loadMarkets();
    const interval = setInterval(loadMarkets, 8000);
    return () => clearInterval(interval);
  }, []);

  async function loadMarkets() {
    try {
      const data = await api.getMarkets("cricket");
      setMarkets(Array.isArray(data) ? data : []);
    } catch {
      // API not available
    } finally {
      setLoading(false);
    }
  }

  // Deduplicate: one card per event (prefer match_odds)
  const deduped = (() => {
    const seen = new Map<string, Market>();
    for (const m of markets) {
      const eventKey = m.event_id || m.id.replace(/-mo$|-bm$|-fancy\d*$|-ou$/, "");
      const existing = seen.get(eventKey);
      if (!existing || (m.market_type === "match_odds" && existing.market_type !== "match_odds")) {
        seen.set(eventKey, m);
      }
    }
    return Array.from(seen.values());
  })();

  const filtered = deduped.filter((m) => {
    if (filter === "live") return m.in_play || m.status === "in_play";
    if (filter === "upcoming") return !m.in_play && m.status !== "in_play";
    return true;
  });

  const liveCount = deduped.filter(
    (m) => m.in_play || m.status === "in_play"
  ).length;

  return (
    <div className="max-w-7xl mx-auto px-3 py-4 space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-bold text-white">Cricket Markets</h1>
          <p className="text-xs text-gray-500">
            {markets.length} markets | {liveCount} live
          </p>
        </div>
      </div>

      {/* Filter Tabs */}
      <div className="flex gap-1 bg-surface rounded-lg p-0.5 w-fit">
        {(["all", "live", "upcoming"] as Filter[]).map((f) => (
          <button
            key={f}
            onClick={() => setFilter(f)}
            className={`px-4 py-1.5 rounded-md text-xs font-medium transition capitalize ${
              filter === f
                ? "bg-surface-lighter text-white"
                : "text-gray-500 hover:text-gray-300"
            }`}
          >
            {f}
            {f === "live" && liveCount > 0 && (
              <span className="ml-1 text-profit">({liveCount})</span>
            )}
          </button>
        ))}
      </div>

      {/* Markets Grid */}
      {loading ? (
        <div className="grid gap-1.5 grid-cols-1 lg:grid-cols-2">
          {Array.from({ length: 6 }).map((_, i) => (
            <div
              key={i}
              className="bg-surface rounded-xl border border-gray-800 h-40 animate-pulse"
            />
          ))}
        </div>
      ) : filtered.length > 0 ? (
        <div className="grid gap-1.5 grid-cols-1 lg:grid-cols-2">
          {filtered.map((market) => (
            <MarketCard key={market.id} market={market} />
          ))}
        </div>
      ) : (
        <div className="text-center py-16">
          <h3 className="text-base font-medium text-gray-400">
            No {filter !== "all" ? filter : ""} markets found
          </h3>
          <p className="text-xs text-gray-400 mt-1">
            {filter === "live"
              ? "No matches are currently in-play"
              : "Check back later for new events"}
          </p>
        </div>
      )}
    </div>
  );
}
