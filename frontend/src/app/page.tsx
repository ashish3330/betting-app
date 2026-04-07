"use client";

import { useEffect, useState, useMemo } from "react";
import { api, Market } from "@/lib/api";
import { deduplicateMarkets } from "@/lib/utils";
import MarketTable from "@/components/MarketTable";
import Link from "next/link";
import { useAuth } from "@/lib/auth";

const SPORTS = [
  { key: "all", label: "All Sports", icon: "" },
  { key: "cricket", label: "Cricket", icon: "🏏" },
  { key: "football", label: "Football", icon: "⚽" },
  { key: "tennis", label: "Tennis", icon: "🎾" },
  { key: "basketball", label: "Basketball", icon: "🏀" },
  { key: "boxing", label: "Boxing", icon: "🥊" },
  { key: "ice_hockey", label: "Ice Hockey", icon: "🏒" },
];

export default function HomePage() {
  const { isLoggedIn } = useAuth();
  const [allMarkets, setAllMarkets] = useState<Market[]>([]);
  const [loading, setLoading] = useState(true);
  const [activeSport, setActiveSport] = useState("all");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadMarkets();
    const interval = setInterval(loadMarkets, 8000);
    return () => clearInterval(interval);
  }, []);

  async function loadMarkets() {
    try {
      const [cricket, football, tennis] = await Promise.all([
        api.getMarkets("cricket").catch(() => []),
        api.getMarkets("football").catch(() => []),
        api.getMarkets("tennis").catch(() => []),
      ]);
      const all = [...(cricket || []), ...(football || []), ...(tennis || [])];
      setAllMarkets(all);
      setError(null);
    } catch {
      setError("Failed to load markets");
    } finally {
      setLoading(false);
    }
  }

  // Filter by sport
  const filtered = useMemo(() => {
    if (activeSport === "all") return allMarkets;
    return allMarkets.filter(
      (m) => m.sport?.toLowerCase() === activeSport.toLowerCase()
    );
  }, [allMarkets, activeSport]);

  // Deduplicate: ONE card per event (prefer match_odds, skip bookmaker/fancy/session duplicates)
  const deduped = useMemo(() => deduplicateMarkets(filtered), [filtered]);

  const liveMarkets = useMemo(
    () => deduped.filter((m) => m.in_play || m.status === "in_play"),
    [deduped]
  );

  const upcomingMarkets = useMemo(
    () =>
      deduped
        .filter((m) => !m.in_play && m.status !== "in_play")
        .sort(
          (a, b) =>
            new Date(a.start_time).getTime() - new Date(b.start_time).getTime()
        ),
    [deduped]
  );

  const totalLive = useMemo(
    () => allMarkets.filter((m) => m.in_play || m.status === "in_play").length,
    [allMarkets]
  );

  const sportCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const m of allMarkets) {
      const s = m.sport?.toLowerCase() || "other";
      counts[s] = (counts[s] || 0) + 1;
    }
    return counts;
  }, [allMarkets]);

  return (
    <div className="min-h-screen">
      {/* Thin lotus gradient accent line */}
      <div className="h-[2px] bg-gradient-to-r from-lotus-dark via-lotus to-lotus-light" />

      <div className="max-w-[1200px] mx-auto px-3 py-3 space-y-3">
        {/* Login/Register is in navbar — no duplicate banner needed */}

        {/* Sport filter tabs */}
        <div className="flex items-center gap-2 border-b border-gray-800/40 overflow-x-auto scrollbar-none">
          {SPORTS.map((s) => {
            const isActive = activeSport === s.key;
            const count =
              s.key === "all"
                ? allMarkets.length
                : sportCounts[s.key] || 0;
            return (
              <button
                key={s.key}
                onClick={() => setActiveSport(s.key)}
                className={`relative flex items-center gap-1.5 px-3 py-2 text-xs font-medium whitespace-nowrap transition-colors ${
                  isActive
                    ? "text-white"
                    : "text-gray-500 hover:text-gray-300"
                }`}
              >
                {s.icon && <span className="text-sm">{s.icon}</span>}
                {s.label}
                {count > 0 && (
                  <span
                    className={`text-[10px] px-1.5 py-0 rounded-full font-mono ${
                      isActive
                        ? "bg-lotus/20 text-lotus"
                        : "bg-white/5 text-gray-500"
                    }`}
                  >
                    {count}
                  </span>
                )}
                {/* Active underline */}
                {isActive && (
                  <span className="absolute bottom-0 left-1 right-1 h-[2px] bg-lotus rounded-full" />
                )}
              </button>
            );
          })}

          {/* Live count -- right aligned */}
          {totalLive > 0 && (
            <div className="flex items-center gap-1.5 ml-auto flex-shrink-0 pr-1">
              <span className="w-1.5 h-1.5 bg-green-500 rounded-full animate-pulse" />
              <span className="text-[11px] text-green-400 font-semibold tabular-nums">
                {totalLive} Live
              </span>
            </div>
          )}
        </div>

        {/* Highlights strip */}
        {liveMarkets.length > 0 && (
          <div className="flex gap-2 overflow-x-auto scrollbar-none pb-1 -mx-1 px-1">
            {liveMarkets.slice(0, 5).map((m) => (
              <Link
                key={`hl-${m.id}`}
                href={`/markets/${m.id}`}
                className="flex items-center gap-2 bg-surface border border-gray-800/40 rounded px-3 py-1.5 flex-shrink-0 hover:border-gray-700 transition group"
              >
                <span className="w-1.5 h-1.5 bg-green-500 rounded-full animate-pulse flex-shrink-0" />
                <span className="text-[11px] text-gray-300 group-hover:text-white transition truncate max-w-[200px]">
                  {m.event_name || m.name}
                </span>
                <span className="text-[10px] text-green-400 font-semibold flex-shrink-0">
                  LIVE
                </span>
              </Link>
            ))}
          </div>
        )}

        {/* Error state */}
        {error && !loading && allMarkets.length === 0 && (
          <div className="text-center py-8 bg-surface rounded border border-gray-800/40">
            <p className="text-xs text-gray-400">{error}</p>
            <button
              onClick={loadMarkets}
              className="text-xs text-lotus hover:text-lotus-light mt-2 transition"
            >
              Retry
            </button>
          </div>
        )}

        {/* Live Markets — table layout like playzone9 */}
        {liveMarkets.length > 0 && (
          <MarketTable markets={liveMarkets} title={`Live (${liveMarkets.length})`} showPositions />
        )}

        {/* Upcoming Markets — table layout */}
        {upcomingMarkets.length > 0 && (
          <MarketTable markets={upcomingMarkets.slice(0, 30)} title={`Upcoming (${upcomingMarkets.length})`} showLiveBadge={false} showPositions />
        )}

        {/* Loading skeleton */}
        {loading && (
          <div className="space-y-3">
            {/* Tab skeleton */}
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-1.5">
              {Array.from({ length: 6 }).map((_, i) => (
                <div
                  key={i}
                  className="bg-surface rounded border border-gray-800/40 h-[120px] animate-pulse"
                />
              ))}
            </div>
          </div>
        )}

        {/* Empty state */}
        {!loading && filtered.length === 0 && !error && (
          <div className="text-center py-16">
            <p className="text-sm text-gray-400">No markets available</p>
            <p className="text-xs text-gray-600 mt-1">
              Check back later or select a different sport
            </p>
          </div>
        )}
      </div>
    </div>
  );
}
