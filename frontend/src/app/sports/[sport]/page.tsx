"use client";

import { useEffect, useState, useMemo, useCallback } from "react";
import { useParams, useSearchParams } from "next/navigation";
import { api, Competition, Market } from "@/lib/api";
import { deduplicateMarkets } from "@/lib/utils";
import MarketTable from "@/components/MarketTable";

type TimeTab = "in_play" | "today" | "future";

export default function SportPage() {
  const params = useParams();
  const searchParams = useSearchParams();
  const sport = params.sport as string;
  const competitionFromURL = searchParams.get("competition");

  const [competitions, setCompetitions] = useState<Competition[]>([]);
  const [markets, setMarkets] = useState<Market[]>([]);
  const [loading, setLoading] = useState(true);
  const [initialLoad, setInitialLoad] = useState(true);
  const [activeComp, setActiveComp] = useState<string>(competitionFromURL || "all");
  const [activeTab, setActiveTab] = useState<TimeTab>("in_play");

  const sportName = sport
    .replace(/-|_/g, " ")
    .replace(/\b\w/g, (c) => c.toUpperCase());

  useEffect(() => {
    if (competitionFromURL) setActiveComp(competitionFromURL);
  }, [competitionFromURL]);

  const loadData = useCallback(
    async (isInitial: boolean) => {
      if (isInitial) setLoading(true);
      try {
        const [comps, mkts] = await Promise.all([
          api.fetchCompetitions(sport.replace(/-/g, "_")).catch(() => []),
          api.getMarkets(sport.replace(/-/g, "_")).catch(() => []),
        ]);
        setCompetitions(Array.isArray(comps) ? comps : []);
        setMarkets(Array.isArray(mkts) ? mkts : []);
      } catch {
        // ignore
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
    loadData(true);
    const interval = setInterval(() => loadData(false), 8000);
    return () => clearInterval(interval);
  }, [loadData]);

  // Deduplicate markets by event (one card per match)
  const deduped = useMemo(() => deduplicateMarkets(markets), [markets]);

  // Filter by competition using actual competition_id/name match
  const compFiltered = useMemo(() => {
    if (activeComp === "all") return deduped;
    const comp = competitions.find((c) => c.id === activeComp);
    return deduped.filter((m) => {
      // Prefer competition name match on the market (Market type only carries competition name).
      if (comp && m.competition && m.competition === comp.name) return true;
      // Fallback: event_id prefix match for legacy data shapes.
      const eventKey = m.event_id || m.id;
      return eventKey.startsWith(activeComp);
    });
  }, [deduped, activeComp, competitions]);

  // Classify events by time
  const { inPlayMarkets, todayMarkets, futureMarkets } = useMemo(() => {
    const now = new Date();
    const startOfDay = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const endOfDay = new Date(startOfDay);
    endOfDay.setDate(endOfDay.getDate() + 1);

    const inPlay: Market[] = [];
    const today: Market[] = [];
    const future: Market[] = [];

    for (const m of compFiltered) {
      const isLive = m.in_play || m.status === "in_play";
      if (isLive) {
        inPlay.push(m);
        continue;
      }
      const t = new Date(m.start_time).getTime();
      if (t >= startOfDay.getTime() && t < endOfDay.getTime()) {
        today.push(m);
      } else if (t >= endOfDay.getTime()) {
        future.push(m);
      } else {
        // Past but not in_play — bucket into today for visibility
        today.push(m);
      }
    }

    const byTime = (a: Market, b: Market) =>
      new Date(a.start_time).getTime() - new Date(b.start_time).getTime();

    return {
      inPlayMarkets: inPlay.sort(byTime),
      todayMarkets: today.sort(byTime),
      futureMarkets: future.sort(byTime),
    };
  }, [compFiltered]);

  const shown =
    activeTab === "in_play"
      ? inPlayMarkets
      : activeTab === "today"
      ? todayMarkets
      : futureMarkets;

  return (
    <div className="max-w-[1200px] mx-auto px-3 py-3 space-y-3">
      {/* Header */}
      <div>
        <h1 className="text-lg font-bold text-white">{sportName}</h1>
        <p className="text-xs text-gray-500">
          {deduped.length} events · {inPlayMarkets.length} in-play
        </p>
      </div>

      {/* Time tabs: In-Play / Today / Future */}
      <div className="flex items-center gap-1 bg-surface rounded-lg p-0.5 w-fit">
        {(
          [
            { k: "in_play" as TimeTab, label: "In-Play", count: inPlayMarkets.length },
            { k: "today" as TimeTab, label: "Today", count: todayMarkets.length },
            { k: "future" as TimeTab, label: "Future", count: futureMarkets.length },
          ]
        ).map((t) => {
          const isActive = activeTab === t.k;
          return (
            <button
              key={t.k}
              onClick={() => setActiveTab(t.k)}
              className={`px-3 py-1.5 rounded-md text-xs font-medium transition ${
                isActive
                  ? "bg-surface-lighter text-white"
                  : "text-gray-500 hover:text-gray-300"
              }`}
            >
              <span className="inline-flex items-center gap-1">
                {t.k === "in_play" && t.count > 0 && (
                  <span className="w-1.5 h-1.5 bg-green-500 rounded-full animate-pulse" />
                )}
                {t.label}
                <span
                  className={`text-[10px] tabular-nums ${
                    isActive ? "text-gray-400" : "text-gray-600"
                  }`}
                >
                  ({t.count})
                </span>
              </span>
            </button>
          );
        })}
      </div>

      {/* Competition tabs */}
      {competitions.length > 0 && (
        <div className="flex items-center gap-1 border-b border-gray-800/40 overflow-x-auto">
          <button
            onClick={() => setActiveComp("all")}
            className={`relative px-3 py-2 text-xs font-medium whitespace-nowrap transition ${
              activeComp === "all" ? "text-white" : "text-gray-500 hover:text-gray-300"
            }`}
          >
            All
            <span className="text-[10px] ml-1 text-gray-500">{deduped.length}</span>
            {activeComp === "all" && (
              <span className="absolute bottom-0 left-1 right-1 h-[2px] bg-lotus rounded-full" />
            )}
          </button>
          {competitions.map((comp) => (
            <button
              key={comp.id}
              onClick={() => setActiveComp(comp.id)}
              className={`relative px-3 py-2 text-xs font-medium whitespace-nowrap transition ${
                activeComp === comp.id ? "text-white" : "text-gray-500 hover:text-gray-300"
              }`}
            >
              {comp.name}
              <span className="text-[10px] ml-1 text-gray-500">
                {comp.match_count || 0}
              </span>
              {activeComp === comp.id && (
                <span className="absolute bottom-0 left-1 right-1 h-[2px] bg-lotus rounded-full" />
              )}
            </button>
          ))}
        </div>
      )}

      {/* Loading — only on first load */}
      {loading && initialLoad && markets.length === 0 && (
        <div className="space-y-1">
          {Array.from({ length: 6 }).map((_, i) => (
            <div
              key={i}
              className="bg-surface border border-gray-800/40 h-14 animate-pulse"
            />
          ))}
        </div>
      )}

      {/* Markets table for the active tab */}
      {shown.length > 0 && (
        <MarketTable
          markets={shown}
          title={
            activeTab === "in_play"
              ? `Live (${shown.length})`
              : activeTab === "today"
              ? `Today (${shown.length})`
              : `Future (${shown.length})`
          }
          showLiveBadge={activeTab === "in_play"}
          showPositions
        />
      )}

      {/* Empty state */}
      {!initialLoad && shown.length === 0 && (
        <div className="text-center py-12 text-gray-500">
          <p className="text-sm">
            No {activeTab === "in_play" ? "in-play" : activeTab} events for {sportName}
          </p>
          <p className="text-xs text-gray-600 mt-1">
            Try switching tabs or come back later
          </p>
        </div>
      )}
    </div>
  );
}
