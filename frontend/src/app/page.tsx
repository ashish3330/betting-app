"use client";

import { useEffect, useState, useMemo, useCallback } from "react";
import { api, Market, SportEvent, LiveScore } from "@/lib/api";
import { deduplicateMarkets } from "@/lib/utils";
import MarketTable from "@/components/MarketTable";
import Link from "next/link";
import { useAuth } from "@/lib/auth";

const SPORT_KEYS = [
  "cricket",
  "football",
  "tennis",
  "basketball",
  "ice_hockey",
  "baseball",
  "boxing",
];

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
  void isLoggedIn;
  const [allMarkets, setAllMarkets] = useState<Market[]>([]);
  const [loading, setLoading] = useState(true);
  const [activeSport, setActiveSport] = useState("all");
  const [error, setError] = useState<string | null>(null);
  const [heroEvent, setHeroEvent] = useState<SportEvent | null>(null);
  const [heroScore, setHeroScore] = useState<LiveScore | null>(null);

  const loadMarkets = useCallback(async () => {
    try {
      const results = await Promise.allSettled(
        SPORT_KEYS.map((s) => api.getMarkets(s))
      );
      const merged: Market[] = [];
      for (const r of results) {
        if (r.status === "fulfilled" && Array.isArray(r.value)) {
          merged.push(...r.value);
        }
      }
      setAllMarkets(merged);
      setError(null);
    } catch {
      setError("Failed to load markets");
    } finally {
      setLoading(false);
    }
  }, []);

  const loadHero = useCallback(async () => {
    try {
      const events = await api.fetchEventsBySport("cricket").catch(() => []);
      if (Array.isArray(events) && events.length > 0) {
        const live = events.find((e) => e.in_play) || null;
        setHeroEvent(live);
        if (live) {
          try {
            const score = await api.getLiveScore(live.id);
            setHeroScore(score);
          } catch {
            setHeroScore(null);
          }
        } else {
          setHeroScore(null);
        }
      } else {
        setHeroEvent(null);
      }
    } catch {
      setHeroEvent(null);
    }
  }, []);

  useEffect(() => {
    loadMarkets();
    loadHero();
    const interval = setInterval(() => {
      loadMarkets();
      loadHero();
    }, 8000);
    return () => clearInterval(interval);
  }, [loadMarkets, loadHero]);

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

  // Popular markets — top 8 by total_matched (deduped across all sports)
  const popularMarkets = useMemo(() => {
    const all = deduplicateMarkets(allMarkets);
    return [...all]
      .sort((a, b) => (b.total_matched || 0) - (a.total_matched || 0))
      .slice(0, 8);
  }, [allMarkets]);

  // Hero market link — first match_odds market for hero event, else /sports
  const heroMarketId = useMemo(() => {
    if (!heroEvent) return null;
    const evId = heroEvent.id;
    const match = allMarkets.find(
      (m) => m.event_id === evId || m.id.startsWith(evId)
    );
    return match?.id || heroEvent.market_id || null;
  }, [heroEvent, allMarkets]);

  return (
    <div className="min-h-screen">
      {/* Thin lotus gradient accent line */}
      <div className="h-[2px] bg-gradient-to-r from-lotus-dark via-lotus to-lotus-light" />

      <div className="max-w-[1200px] mx-auto px-3 py-3 space-y-3">
        {/* Hero section */}
        {heroEvent ? (
          <Link
            href={heroMarketId ? `/markets/${heroMarketId}` : "/sports/cricket"}
            className="block group"
          >
            <div className="relative overflow-hidden rounded-lg border border-lotus/20 bg-gradient-to-br from-lotus-dark/40 via-surface to-black p-4 sm:p-5 transition hover:border-lotus/40">
              <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_top_right,rgba(236,72,153,0.15),transparent_60%)]" />
              <div className="relative flex flex-col sm:flex-row sm:items-center gap-3 sm:gap-5">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1.5">
                    <span className="inline-flex items-center gap-1 bg-red-500/15 text-red-400 text-[10px] font-bold px-1.5 py-0.5 rounded">
                      <span className="w-1.5 h-1.5 bg-red-500 rounded-full animate-pulse" />
                      LIVE
                    </span>
                    <span className="text-[11px] text-gray-400 truncate">
                      {heroEvent.competition || "Cricket"}
                    </span>
                  </div>
                  <h2 className="text-base sm:text-xl font-bold text-white leading-tight truncate">
                    {heroEvent.name}
                  </h2>
                  {heroScore && (heroScore.home_score || heroScore.away_score) && (
                    <div className="mt-1.5 flex items-center gap-3 text-xs text-gray-300 tabular-nums">
                      <span className="font-mono">
                        {heroScore.home} {heroScore.home_score}
                      </span>
                      <span className="text-gray-600">vs</span>
                      <span className="font-mono">
                        {heroScore.away} {heroScore.away_score}
                      </span>
                      {heroScore.overs && (
                        <span className="text-[10px] text-gray-500">
                          ({heroScore.overs})
                        </span>
                      )}
                    </div>
                  )}
                </div>
                <div className="flex-shrink-0">
                  <span className="inline-flex items-center gap-1.5 bg-lotus hover:bg-lotus-light text-white text-xs font-bold px-4 py-2 rounded transition">
                    Bet Now
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M5 12h14M12 5l7 7-7 7" />
                    </svg>
                  </span>
                </div>
              </div>
            </div>
          </Link>
        ) : !loading ? (
          <div className="relative overflow-hidden rounded-lg border border-gray-800/40 bg-gradient-to-br from-surface via-surface-lighter to-black p-4 sm:p-5">
            <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_top_right,rgba(236,72,153,0.12),transparent_60%)]" />
            <div className="relative flex flex-col sm:flex-row sm:items-center gap-3 sm:gap-5">
              <div className="flex-1 min-w-0">
                <h2 className="text-base sm:text-xl font-bold text-white leading-tight">
                  Welcome to Lotus Exchange
                </h2>
                <p className="mt-1 text-xs text-gray-400">
                  Browse sports, markets and place your bets on the world&apos;s best betting exchange
                </p>
              </div>
              <div className="flex gap-2 flex-shrink-0">
                <Link
                  href="/sports"
                  className="inline-flex items-center bg-lotus hover:bg-lotus-light text-white text-xs font-bold px-4 py-2 rounded transition"
                >
                  Browse Sports
                </Link>
                <Link
                  href="/casino"
                  className="inline-flex items-center border border-gray-700 hover:border-lotus text-gray-300 hover:text-white text-xs font-bold px-4 py-2 rounded transition"
                >
                  Casino
                </Link>
              </div>
            </div>
          </div>
        ) : null}

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
                aria-label={`Filter by ${s.label}`}
                aria-pressed={isActive}
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

        {/* Popular Markets carousel */}
        {popularMarkets.length > 0 && (
          <div>
            <div className="flex items-center justify-between mb-1.5 px-0.5">
              <h3 className="text-[11px] font-bold text-gray-400 uppercase tracking-wider">
                Popular Markets
              </h3>
              <Link href="/markets" className="text-[10px] text-lotus hover:text-lotus-light transition">
                View all
              </Link>
            </div>
            <div className="flex gap-2 overflow-x-auto scrollbar-none pb-1 -mx-1 px-1">
              {popularMarkets.map((m) => {
                const runner = m.runners?.[0];
                const back = runner?.back_prices?.[0] || { price: runner?.back_price || 0 };
                const lay = runner?.lay_prices?.[0] || { price: runner?.lay_price || 0 };
                const isLive = m.in_play || m.status === "in_play";
                return (
                  <Link
                    key={`pop-${m.id}`}
                    href={`/markets/${m.id}`}
                    className="flex-shrink-0 w-[240px] bg-surface border border-gray-800/40 rounded hover:border-lotus/40 transition group"
                  >
                    <div className="px-2.5 py-2 border-b border-gray-800/30">
                      <div className="flex items-center gap-1.5 mb-1">
                        {isLive ? (
                          <span className="inline-flex items-center gap-1 text-[9px] text-green-400 font-bold">
                            <span className="w-1 h-1 bg-green-500 rounded-full animate-pulse" />
                            LIVE
                          </span>
                        ) : (
                          <span className="text-[9px] text-gray-500 uppercase">{m.sport}</span>
                        )}
                        <span className="text-[9px] text-gray-600 truncate">
                          {m.competition || ""}
                        </span>
                      </div>
                      <p className="text-[11px] font-semibold text-white truncate group-hover:text-lotus-light transition">
                        {m.event_name || m.name}
                      </p>
                    </div>
                    <div className="flex items-stretch">
                      <div className="flex-1 bg-[#72BBEF]/90 text-center py-1.5">
                        <div className="text-[9px] text-black/60 leading-none">Back</div>
                        <div className="text-[11px] font-bold text-black tabular-nums leading-tight">
                          {back.price > 0 ? back.price.toFixed(2) : "-"}
                        </div>
                      </div>
                      <div className="flex-1 bg-[#FAA9BA]/90 text-center py-1.5">
                        <div className="text-[9px] text-black/60 leading-none">Lay</div>
                        <div className="text-[11px] font-bold text-black tabular-nums leading-tight">
                          {lay.price > 0 ? lay.price.toFixed(2) : "-"}
                        </div>
                      </div>
                    </div>
                  </Link>
                );
              })}
            </div>
          </div>
        )}

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
