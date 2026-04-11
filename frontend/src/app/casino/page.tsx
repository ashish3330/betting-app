"use client";

import { useToast } from "@/components/Toast";
import { useEffect, useState } from "react";
import { api, CasinoGame } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import Link from "next/link";
import {
  CASINO_CATEGORIES,
  CASINO_PROVIDERS,
  CASINO_GAMES,
  CasinoGameItem,
  GameCategory,
  canonicalCategory,
} from "@/lib/casino-games";
import { CasinoGameTile } from "@/components/CasinoGameTile";

/* ------------------------------------------------------------------ */
/*  Component                                                          */
/* ------------------------------------------------------------------ */

export default function CasinoPage() {
  const { isLoggedIn } = useAuth();
  const { addToast } = useToast();
  const [games, setGames] = useState<CasinoGame[]>([]);
  const [loading, setLoading] = useState(true);
  const [launchingId, setLaunchingId] = useState<string | null>(null);
  const [activeCategory, setActiveCategory] = useState<GameCategory | "all">("all");
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

  const allGames: CasinoGameItem[] = games.length > 0
    ? games.map((g) => {
        const f = CASINO_GAMES.find((fg) => fg.type === g.type);
        const cat = canonicalCategory(g.category || f?.category || "live_casino");
        return {
          id: g.id,
          name: g.name,
          type: g.type,
          provider_id: g.provider_id,
          provider_name: g.provider_name,
          is_live: g.is_live,
          image: g.image ?? null,
          icon: f?.icon || g.name.slice(0, 2).toUpperCase(),
          category: (cat === "all" ? "live_casino" : cat) as GameCategory,
          badge: f?.badge ?? null,
          min_bet: f?.min_bet ?? 50,
        };
      })
    : CASINO_GAMES;

  const filteredGames = allGames.filter((g) => {
    const matchCat = activeCategory === "all" || g.category === activeCategory;
    const matchProv = activeProvider === "all" || g.provider_id === activeProvider;
    const matchSearch = !searchQuery || g.name.toLowerCase().includes(searchQuery.toLowerCase());
    return matchCat && matchProv && matchSearch;
  });

  async function launchGame(game: CasinoGameItem) {
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
          {CASINO_CATEGORIES.map((cat) => (
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
          {CASINO_PROVIDERS.map((prov) => (
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
              <CasinoGameTile
                key={game.id}
                game={game}
                launching={launchingId === game.id}
                onLaunch={launchGame}
              />
            ))}
          </div>
        )}

        {/* Category links */}
        <div className="border-t border-gray-800 pt-4">
          <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wider mb-3">Browse by Category</h3>
          <div className="flex flex-wrap gap-2">
            {CASINO_CATEGORIES.filter((c) => c.id !== "all").map((cat) => (
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
