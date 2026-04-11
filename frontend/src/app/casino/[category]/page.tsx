"use client";

import { useToast } from "@/components/Toast";
import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { api, CasinoGame } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import Link from "next/link";
import {
  CASINO_GAMES,
  CASINO_PROVIDERS,
  CASINO_CATEGORY_META,
  CATEGORY_ALIAS,
  CasinoGameItem,
  GameCategory,
  canonicalCategory,
} from "@/lib/casino-games";
import { CasinoGameTile } from "@/components/CasinoGameTile";

/* ------------------------------------------------------------------ */
/*  Component                                                          */
/* ------------------------------------------------------------------ */

export default function CasinoCategoryPage() {
  const { addToast } = useToast();
  const params = useParams();
  const router = useRouter();
  const rawCategory = (params.category as string) || "";
  const { isLoggedIn } = useAuth();

  // Redirect legacy duplicates to the canonical slug.
  useEffect(() => {
    const canon = CATEGORY_ALIAS[rawCategory];
    if (canon && canon !== rawCategory) {
      router.replace(`/casino/${canon}`);
    }
  }, [rawCategory, router]);

  const category = canonicalCategory(rawCategory);
  const categoryKey = (category === "all" ? "live_casino" : category) as GameCategory;

  const [apiGames, setApiGames] = useState<CasinoGame[]>([]);
  const [loading, setLoading] = useState(true);
  const [launchingId, setLaunchingId] = useState<string | null>(null);
  const [activeProvider, setActiveProvider] = useState("all");
  const [searchQuery, setSearchQuery] = useState("");

  const meta =
    CASINO_CATEGORY_META[categoryKey] || {
      title: rawCategory?.replace(/_/g, " ").replace(/\b\w/g, (c: string) => c.toUpperCase()) || "Games",
      description: "Browse casino games",
    };

  useEffect(() => {
    loadGames();
  }, [categoryKey]);

  async function loadGames() {
    setLoading(true);
    try {
      const data = await api.fetchGamesByCategory(categoryKey);
      setApiGames(Array.isArray(data) ? data : []);
    } catch {
      // fallback to static data
    } finally {
      setLoading(false);
    }
  }

  const staticGames = CASINO_GAMES.filter((g) => g.category === categoryKey);

  const allGames: CasinoGameItem[] =
    apiGames.length > 0
      ? apiGames.map((g) => {
          const f = CASINO_GAMES.find((fg) => fg.type === g.type);
          const cat = canonicalCategory(g.category || f?.category || categoryKey);
          return {
            id: g.id,
            name: g.name,
            type: g.type,
            provider_id: g.provider_id,
            provider_name: g.provider_name,
            is_live: g.is_live,
            image: g.image ?? null,
            icon: f?.icon || g.name.slice(0, 2).toUpperCase(),
            category: (cat === "all" ? categoryKey : cat) as GameCategory,
            badge: f?.badge ?? null,
            min_bet: f?.min_bet ?? 50,
          };
        })
      : staticGames;

  const filteredGames = allGames.filter((g) => {
    const matchProv = activeProvider === "all" || g.provider_id === activeProvider;
    const matchSearch = !searchQuery || g.name.toLowerCase().includes(searchQuery.toLowerCase());
    return matchProv && matchSearch;
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
        {/* Breadcrumb */}
        <div className="flex items-center gap-2 text-xs text-gray-500">
          <Link href="/" className="hover:text-white transition">Home</Link>
          <span>/</span>
          <Link href="/casino" className="hover:text-white transition">Casino</Link>
          <span>/</span>
          <span className="text-white">{meta.title}</span>
        </div>

        {/* Header */}
        <div>
          <h1 className="text-xl font-bold text-white">{meta.title}</h1>
          <p className="text-xs text-gray-500 mt-0.5">{meta.description}</p>
          <p className="text-[10px] text-gray-600 mt-1">
            {filteredGames.length} games &middot; {new Set(filteredGames.map((g) => g.provider_name)).size} providers
          </p>
        </div>

        {/* Filters */}
        <div className="flex flex-col sm:flex-row gap-3 sm:items-center sm:justify-between">
          {/* Search */}
          <div className="relative">
            <svg className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <input
              type="text"
              placeholder={`Search ${meta.title.toLowerCase()}...`}
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="w-full sm:w-72 bg-surface border border-gray-800 rounded-lg pl-10 pr-4 py-2 text-sm text-white placeholder-gray-500 focus:outline-none focus:border-gray-600 transition"
            />
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
                    : "bg-surface border border-gray-800 text-gray-500 hover:text-gray-300 hover:border-gray-700"
                }`}
              >
                {prov.name}
              </button>
            ))}
          </div>
        </div>

        {/* Games grid */}
        {loading ? (
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-3 sm:gap-4">
            {Array.from({ length: 8 }).map((_, i) => (
              <div key={i} className="bg-surface rounded-lg border border-gray-800 h-48 animate-pulse" />
            ))}
          </div>
        ) : filteredGames.length === 0 ? (
          <div className="text-center py-16">
            <h3 className="text-base font-medium text-gray-400">No games found</h3>
            <p className="text-sm text-gray-500 mt-1">Try a different filter or search term</p>
            <Link
              href="/casino"
              className="inline-flex items-center gap-2 mt-5 text-sm text-gray-400 hover:text-white transition"
            >
              Back to Casino
            </Link>
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

        {/* Navigation */}
        <div className="flex items-center justify-between border-t border-gray-800 pt-4">
          <Link
            href="/casino"
            className="inline-flex items-center gap-2 text-sm text-gray-400 hover:text-white transition font-medium"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" /></svg>
            All Casino Games
          </Link>

          <div className="hidden sm:flex items-center gap-2">
            {Object.entries(CASINO_CATEGORY_META)
              .filter(([key]) => key !== categoryKey)
              .slice(0, 3)
              .map(([key, val]) => (
                <Link
                  key={key}
                  href={`/casino/${key}`}
                  className="text-xs text-gray-500 hover:text-white transition px-3 py-1.5 rounded-md bg-surface border border-gray-800 hover:border-gray-700"
                >
                  {val.title}
                </Link>
              ))}
          </div>
        </div>
      </div>
    </div>
  );
}
