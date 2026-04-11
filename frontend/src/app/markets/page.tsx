"use client";

import { useEffect, useState, useMemo, useCallback, Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { api, Market } from "@/lib/api";
import { deduplicateMarkets } from "@/lib/utils";
import MarketCard from "@/components/MarketCard";

type Filter = "all" | "live" | "upcoming";

const SPORT_TABS = [
  { key: "cricket", label: "Cricket", icon: "🏏" },
  { key: "football", label: "Football", icon: "⚽" },
  { key: "tennis", label: "Tennis", icon: "🎾" },
  { key: "basketball", label: "Basketball", icon: "🏀" },
  { key: "ice_hockey", label: "Ice Hockey", icon: "🏒" },
  { key: "baseball", label: "Baseball", icon: "⚾" },
  { key: "boxing", label: "Boxing", icon: "🥊" },
];

function MarketsInner() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const sport = (searchParams.get("sport") || "cricket").toLowerCase();

  const [markets, setMarkets] = useState<Market[]>([]);
  const [loading, setLoading] = useState(true);
  const [initialLoad, setInitialLoad] = useState(true);
  const [filter, setFilter] = useState<Filter>("all");

  const loadMarkets = useCallback(
    async (isInitial: boolean) => {
      if (isInitial) setLoading(true);
      try {
        const data = await api.getMarkets(sport);
        setMarkets(Array.isArray(data) ? data : []);
      } catch {
        setMarkets([]);
      } finally {
        if (isInitial) {
          setLoading(false);
          setInitialLoad(false);
        }
      }
    },
    [sport]
  );

  useEffect(() => {
    setInitialLoad(true);
    loadMarkets(true);
    const interval = setInterval(() => loadMarkets(false), 8000);
    return () => clearInterval(interval);
  }, [loadMarkets]);

  const deduped = useMemo(() => deduplicateMarkets(markets), [markets]);

  const filtered = useMemo(
    () =>
      deduped.filter((m) => {
        if (filter === "live") return m.in_play || m.status === "in_play";
        if (filter === "upcoming") return !m.in_play && m.status !== "in_play";
        return true;
      }),
    [deduped, filter]
  );

  const liveCount = useMemo(
    () =>
      deduped.filter((m) => m.in_play || m.status === "in_play").length,
    [deduped]
  );

  const sportLabel =
    SPORT_TABS.find((t) => t.key === sport)?.label ||
    sport.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());

  const handleSportChange = (key: string) => {
    router.push(`/markets?sport=${key}`);
  };

  return (
    <div className="max-w-[1200px] mx-auto px-3 py-4 space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-bold text-white">{sportLabel} Markets</h1>
          <p className="text-xs text-gray-500">
            {deduped.length} markets · {liveCount} live
          </p>
        </div>
      </div>

      {/* Sport tabs */}
      <div className="flex items-center gap-2 border-b border-gray-800/40 overflow-x-auto scrollbar-none">
        {SPORT_TABS.map((s) => {
          const isActive = sport === s.key;
          return (
            <button
              key={s.key}
              onClick={() => handleSportChange(s.key)}
              className={`relative flex items-center gap-1.5 px-3 py-2 text-xs font-medium whitespace-nowrap transition-colors ${
                isActive ? "text-white" : "text-gray-500 hover:text-gray-300"
              }`}
            >
              <span className="text-sm">{s.icon}</span>
              {s.label}
              {isActive && (
                <span className="absolute bottom-0 left-1 right-1 h-[2px] bg-lotus rounded-full" />
              )}
            </button>
          );
        })}
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
      {loading && initialLoad ? (
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
            No {filter !== "all" ? filter : ""} {sportLabel.toLowerCase()} markets found
          </h3>
          <p className="text-xs text-gray-400 mt-1">
            {filter === "live"
              ? "No matches are currently in-play"
              : "Check back later or switch sport"}
          </p>
        </div>
      )}
    </div>
  );
}

export default function MarketsPage() {
  return (
    <Suspense
      fallback={
        <div className="max-w-[1200px] mx-auto px-3 py-4">
          <div className="h-6 w-40 bg-surface animate-pulse rounded" />
        </div>
      }
    >
      <MarketsInner />
    </Suspense>
  );
}
