"use client";

import { useToast } from "@/components/Toast";
import { useEffect, useState } from "react";
import { api, CasinoGame } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import Link from "next/link";

/* ------------------------------------------------------------------ */
/*  Static data                                                        */
/* ------------------------------------------------------------------ */

const CATEGORIES = [
  { id: "all", name: "All Games" },
  { id: "live_casino", name: "Live Casino" },
  { id: "virtual", name: "Virtual Sports" },
  { id: "slots", name: "Slots" },
  { id: "crash", name: "Crash Games" },
  { id: "card", name: "Card Games" },
];

const PROVIDERS = [
  { id: "all", name: "All Providers" },
  { id: "evolution", name: "Evolution" },
  { id: "ezugi", name: "Ezugi" },
  { id: "betgames", name: "BetGames" },
  { id: "superspade", name: "Super Spade" },
  { id: "tvbet", name: "TVBet" },
];

interface GameItem {
  id: string;
  name: string;
  type: string;
  provider_id: string;
  provider_name: string;
  is_live: boolean;
  image?: string | null;
  icon: string;
  category: string;
  badge?: "NEW" | "HOT" | null;
  min_bet?: number;
}

const STATIC_GAMES: GameItem[] = [
  { id: "teen-patti", name: "Teen Patti", type: "teen_patti", provider_id: "ezugi", provider_name: "Ezugi", is_live: true, image: null, icon: "TP", category: "live_casino", badge: "HOT", min_bet: 100 },
  { id: "andar-bahar", name: "Andar Bahar", type: "andar_bahar", provider_id: "evolution", provider_name: "Evolution", is_live: true, image: null, icon: "AB", category: "live_casino", badge: "HOT", min_bet: 100 },
  { id: "dragon-tiger", name: "Dragon Tiger", type: "dragon_tiger", provider_id: "ezugi", provider_name: "Ezugi", is_live: true, image: null, icon: "DT", category: "live_casino", badge: null, min_bet: 50 },
  { id: "roulette", name: "Auto Roulette", type: "roulette", provider_id: "evolution", provider_name: "Evolution", is_live: true, image: null, icon: "RO", category: "live_casino", badge: null, min_bet: 50 },
  { id: "baccarat", name: "Baccarat", type: "baccarat", provider_id: "evolution", provider_name: "Evolution", is_live: true, image: null, icon: "BA", category: "live_casino", badge: null, min_bet: 100 },
  { id: "32-card", name: "32 Card Casino", type: "32_card", provider_id: "ezugi", provider_name: "Ezugi", is_live: true, image: null, icon: "32", category: "card", badge: "NEW", min_bet: 50 },
  { id: "lucky7", name: "Lucky 7", type: "lucky7", provider_id: "betgames", provider_name: "BetGames", is_live: true, image: null, icon: "L7", category: "live_casino", badge: null, min_bet: 50 },
  { id: "poker", name: "Casino Hold'em", type: "poker", provider_id: "evolution", provider_name: "Evolution", is_live: true, image: null, icon: "PK", category: "card", badge: null, min_bet: 200 },
  { id: "crash-aviator", name: "Aviator", type: "aviator", provider_id: "betgames", provider_name: "BetGames", is_live: false, image: null, icon: "AV", category: "crash", badge: "HOT", min_bet: 10 },
  { id: "virtual-cricket", name: "Virtual Cricket", type: "virtual_cricket", provider_id: "tvbet", provider_name: "TVBet", is_live: false, image: null, icon: "VC", category: "virtual", badge: "NEW", min_bet: 20 },
  { id: "slots-golden", name: "Golden Fortune", type: "slots_golden", provider_id: "superspade", provider_name: "Super Spade", is_live: false, image: null, icon: "GF", category: "slots", badge: null, min_bet: 10 },
  { id: "hi-lo", name: "Hi-Lo", type: "hi_lo", provider_id: "ezugi", provider_name: "Ezugi", is_live: true, image: null, icon: "HL", category: "card", badge: null, min_bet: 50 },
];

/* ------------------------------------------------------------------ */
/*  Component                                                          */
/* ------------------------------------------------------------------ */

export default function CasinoPage() {
  const { isLoggedIn } = useAuth();
  const { addToast } = useToast();
  const [games, setGames] = useState<CasinoGame[]>([]);
  const [loading, setLoading] = useState(true);
  const [launchingId, setLaunchingId] = useState<string | null>(null);
  const [activeCategory, setActiveCategory] = useState("all");
  const [activeProvider, setActiveProvider] = useState("all");
  const [searchQuery, setSearchQuery] = useState("");

  useEffect(() => {
    loadGames();
  }, []);

  async function loadGames() {
    try {
      const data = await api.getCasinoGames();
      setGames(Array.isArray(data) ? data : []);
    } catch {
      // Use static games as fallback
    } finally {
      setLoading(false);
    }
  }

  const allGames: GameItem[] = games.length > 0
    ? games.map((g) => {
        const f = STATIC_GAMES.find((fg) => fg.type === g.type);
        return {
          ...g,
          icon: f?.icon || g.name.slice(0, 2).toUpperCase(),
          category: g.category || f?.category || "live_casino",
          badge: f?.badge || null,
          min_bet: f?.min_bet || 50,
        };
      })
    : STATIC_GAMES;

  const filteredGames = allGames.filter((g) => {
    const matchCat = activeCategory === "all" || g.category === activeCategory;
    const matchProv = activeProvider === "all" || g.provider_id === activeProvider;
    const matchSearch = !searchQuery || g.name.toLowerCase().includes(searchQuery.toLowerCase());
    return matchCat && matchProv && matchSearch;
  });

  async function launchGame(game: GameItem | CasinoGame) {
    if (!isLoggedIn) {
      window.location.href = "/login";
      return;
    }
    setLaunchingId(game.id);
    try {
      const session = await api.createCasinoSession(game.type, game.provider_id);
      if (session.url) {
        window.open(session.url, "_blank");
      }
    } catch {
      addToast({ type: "error", title: "Failed to launch game" });
    } finally {
      setLaunchingId(null);
    }
  }

  return (
    <div className="min-h-screen">
      <div className="max-w-7xl mx-auto px-4 py-6 space-y-6">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-xl font-bold text-white">Casino Games</h1>
            <p className="text-xs text-gray-500 mt-0.5">
              {filteredGames.length} games available
            </p>
          </div>
        </div>

        {/* Search bar */}
        <div className="relative">
          <svg className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
          <input
            type="text"
            placeholder="Search games..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full sm:w-80 bg-surface border border-gray-800 rounded-lg pl-10 pr-4 py-2 text-sm text-white placeholder-gray-500 focus:outline-none focus:border-gray-600 transition"
          />
        </div>

        {/* Category tabs */}
        <div className="flex gap-2 overflow-x-auto pb-1" style={{ scrollbarWidth: "none" }}>
          {CATEGORIES.map((cat) => (
            <button
              key={cat.id}
              onClick={() => setActiveCategory(cat.id)}
              className={`px-4 py-2 rounded-lg text-sm font-medium whitespace-nowrap transition ${
                activeCategory === cat.id
                  ? "bg-white/10 text-white border border-gray-600"
                  : "bg-surface border border-gray-800 text-gray-400 hover:text-white hover:border-gray-700"
              }`}
            >
              {cat.name}
            </button>
          ))}
        </div>

        {/* Provider filter */}
        <div className="flex gap-2 overflow-x-auto pb-1" style={{ scrollbarWidth: "none" }}>
          {PROVIDERS.map((prov) => (
            <button
              key={prov.id}
              onClick={() => setActiveProvider(prov.id)}
              className={`px-3 py-1.5 rounded-md text-xs font-medium whitespace-nowrap transition ${
                activeProvider === prov.id
                  ? "bg-white/10 text-white border border-gray-600"
                  : "text-gray-500 hover:text-gray-300 border border-transparent"
              }`}
            >
              {prov.name}
            </button>
          ))}
        </div>

        {/* Games grid */}
        {loading ? (
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-3 sm:gap-4">
            {Array.from({ length: 10 }).map((_, i) => (
              <div key={i} className="bg-surface rounded-lg border border-gray-800 h-48 animate-pulse" />
            ))}
          </div>
        ) : filteredGames.length === 0 ? (
          <div className="text-center py-16">
            <h3 className="text-base font-medium text-gray-400">No games found</h3>
            <p className="text-sm text-gray-500 mt-1">Try a different category or search term</p>
          </div>
        ) : (
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-3 sm:gap-4">
            {filteredGames.map((game) => (
              <button
                key={game.id}
                onClick={() => launchGame(game)}
                disabled={launchingId === game.id}
                className="text-left group"
              >
                <div className="bg-surface rounded-lg border border-gray-800 h-36 sm:h-40 p-3 sm:p-4 flex flex-col justify-between transition hover:border-gray-600">
                  {/* Top row: provider + badges */}
                  <div className="flex items-start justify-between">
                    <span className="bg-gray-800 text-gray-400 text-[9px] sm:text-[10px] font-semibold px-2 py-0.5 rounded uppercase tracking-wider">
                      {game.provider_name}
                    </span>
                    <div className="flex flex-col items-end gap-1">
                      {game.is_live && (
                        <span className="bg-red-500/20 text-red-400 text-[9px] font-bold px-1.5 py-0.5 rounded flex items-center gap-1">
                          <span className="w-1 h-1 bg-red-500 rounded-full" />
                          LIVE
                        </span>
                      )}
                      {game.badge === "NEW" && (
                        <span className="bg-emerald-500/20 text-emerald-400 text-[9px] font-bold px-1.5 py-0.5 rounded">NEW</span>
                      )}
                      {game.badge === "HOT" && (
                        <span className="bg-orange-500/20 text-orange-400 text-[9px] font-bold px-1.5 py-0.5 rounded">HOT</span>
                      )}
                    </div>
                  </div>

                  {/* Center icon */}
                  <div className="flex-1 flex items-center justify-center">
                    <div className="text-3xl sm:text-4xl font-black text-gray-600 select-none">
                      {game.icon}
                    </div>
                    {launchingId === game.id && (
                      <div className="absolute inset-0 flex items-center justify-center bg-black/40 rounded-lg">
                        <div className="w-5 h-5 border-2 border-gray-500 border-t-white rounded-full animate-spin" />
                      </div>
                    )}
                  </div>
                </div>

                {/* Game info below card */}
                <div className="mt-2 px-1">
                  <h3 className="text-sm font-medium text-white truncate group-hover:text-gray-300 transition-colors">
                    {game.name}
                  </h3>
                  <div className="flex items-center justify-between mt-0.5">
                    <p className="text-[10px] text-gray-500">{game.provider_name}</p>
                    {game.min_bet && (
                      <p className="text-[10px] text-gray-500">Min: {game.min_bet}</p>
                    )}
                  </div>
                </div>
              </button>
            ))}
          </div>
        )}

        {/* Category links */}
        <div className="border-t border-gray-800 pt-4">
          <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wider mb-3">Browse by Category</h3>
          <div className="flex flex-wrap gap-2">
            {CATEGORIES.filter((c) => c.id !== "all").map((cat) => (
              <Link
                key={cat.id}
                href={`/casino/${cat.id}`}
                className="text-xs text-gray-400 hover:text-white transition px-3 py-1.5 rounded-md bg-surface border border-gray-800 hover:border-gray-700"
              >
                {cat.name}
              </Link>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
