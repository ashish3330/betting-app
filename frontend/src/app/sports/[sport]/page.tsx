"use client";

import { useEffect, useState } from "react";
import { useParams, useSearchParams } from "next/navigation";
import { api, Competition, SportEvent, Market } from "@/lib/api";
import { deduplicateMarkets } from "@/lib/utils";
import MarketTable from "@/components/MarketTable";

export default function SportPage() {
  const params = useParams();
  const searchParams = useSearchParams();
  const sport = params.sport as string;
  const competitionFromURL = searchParams.get("competition");

  const [competitions, setCompetitions] = useState<Competition[]>([]);
  const [markets, setMarkets] = useState<Market[]>([]);
  const [loading, setLoading] = useState(true);
  const [activeComp, setActiveComp] = useState<string>(competitionFromURL || "all");

  const sportName = sport.replace(/-|_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());

  useEffect(() => {
    if (competitionFromURL) setActiveComp(competitionFromURL);
  }, [competitionFromURL]);

  useEffect(() => {
    loadData();
    const interval = setInterval(loadData, 8000);
    return () => clearInterval(interval);
  }, [sport]);

  async function loadData() {
    setLoading(true);
    try {
      const [comps, mkts] = await Promise.all([
        api.fetchCompetitions(sport.replace(/-/g, "_")).catch(() => []),
        api.getMarkets(sport.replace(/-/g, "_")).catch(() => []),
      ]);
      setCompetitions(Array.isArray(comps) ? comps : []);
      setMarkets(Array.isArray(mkts) ? mkts : []);
    } catch {} finally { setLoading(false); }
  }

  // Deduplicate markets by event (one card per match)
  const deduped = deduplicateMarkets(markets);

  // Filter by competition if selected
  const filtered = activeComp === "all" ? deduped : deduped.filter((m) => {
    // Match by event_id prefix or competition name
    const eventKey = m.event_id || m.id;
    return eventKey.includes(activeComp) || m.competition === competitions.find(c => c.id === activeComp)?.name;
  });

  const live = filtered.filter((m) => m.in_play || m.status === "in_play");
  const upcoming = filtered.filter((m) => !m.in_play && m.status !== "in_play");

  return (
    <div className="max-w-[1200px] mx-auto px-3 py-3 space-y-3">
      {/* Competition tabs — horizontal, like homepage sport tabs */}
      <div className="flex items-center gap-1 border-b border-gray-800/40 overflow-x-auto">
        <button
          onClick={() => setActiveComp("all")}
          className={`relative px-3 py-2 text-xs font-medium whitespace-nowrap transition ${
            activeComp === "all" ? "text-white" : "text-gray-500 hover:text-gray-300"
          }`}
        >
          All {sportName}
          <span className="text-[10px] ml-1 text-gray-500">{deduped.length}</span>
          {activeComp === "all" && <span className="absolute bottom-0 left-1 right-1 h-[2px] bg-lotus rounded-full" />}
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
            <span className="text-[10px] ml-1 text-gray-500">{comp.match_count || 0}</span>
            {activeComp === comp.id && <span className="absolute bottom-0 left-1 right-1 h-[2px] bg-lotus rounded-full" />}
          </button>
        ))}

        {live.length > 0 && (
          <div className="flex items-center gap-1.5 ml-auto flex-shrink-0 pr-1">
            <span className="w-1.5 h-1.5 bg-green-500 rounded-full animate-pulse" />
            <span className="text-[11px] text-green-400 font-semibold">{live.length} Live</span>
          </div>
        )}
      </div>

      {/* Loading */}
      {loading && markets.length === 0 && (
        <div className="space-y-1">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="bg-surface border border-gray-800/40 h-14 animate-pulse" />
          ))}
        </div>
      )}

      {/* Live markets table */}
      {live.length > 0 && (
        <MarketTable markets={live} title={`Live (${live.length})`} showPositions />
      )}

      {/* Upcoming markets table */}
      {upcoming.length > 0 && (
        <MarketTable markets={upcoming} title={`Upcoming (${upcoming.length})`} showLiveBadge={false} showPositions />
      )}

      {/* Empty state */}
      {!loading && filtered.length === 0 && (
        <div className="text-center py-12 text-gray-500">
          <p className="text-sm">No events for {sportName}</p>
        </div>
      )}
    </div>
  );
}
