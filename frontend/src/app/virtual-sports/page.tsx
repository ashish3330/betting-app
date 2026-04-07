"use client";

import { useToast } from "@/components/Toast";
import { useState } from "react";
import { useAuth } from "@/lib/auth";
import { api } from "@/lib/api";

const VIRTUAL_SPORTS = [
  { id: "virtual-cricket", name: "Virtual Cricket", type: "virtual_cricket", provider_id: "betgames", provider_name: "BetGames", icon: "VC", color: "from-green-600 to-emerald-700", description: "Simulated cricket matches with real-time odds" },
  { id: "virtual-football", name: "Virtual Football", type: "virtual_football", provider_id: "betgames", provider_name: "BetGames", icon: "VF", color: "from-blue-600 to-indigo-700", description: "AI-powered football simulations" },
  { id: "virtual-tennis", name: "Virtual Tennis", type: "virtual_tennis", provider_id: "betgames", provider_name: "BetGames", icon: "VT", color: "from-yellow-500 to-amber-600", description: "Fast-paced virtual tennis matches" },
  { id: "virtual-horse-racing", name: "Virtual Horse Racing", type: "virtual_horse", provider_id: "betgames", provider_name: "BetGames", icon: "VH", color: "from-amber-600 to-orange-700", description: "Exciting virtual horse races every 3 minutes" },
  { id: "virtual-greyhound", name: "Virtual Greyhound", type: "virtual_greyhound", provider_id: "betgames", provider_name: "BetGames", icon: "VG", color: "from-gray-500 to-gray-700", description: "Virtual greyhound racing with fixed odds" },
  { id: "virtual-speedway", name: "Virtual Speedway", type: "virtual_speedway", provider_id: "betgames", provider_name: "BetGames", icon: "VS", color: "from-red-500 to-rose-700", description: "High-speed motorcycle racing simulations" },
];

export default function VirtualSportsPage() {
  const { isLoggedIn } = useAuth();
  const { addToast } = useToast();
  const [launchingId, setLaunchingId] = useState<string | null>(null);

  async function launchGame(game: typeof VIRTUAL_SPORTS[0]) {
    if (!isLoggedIn) {
      window.location.href = "/login";
      return;
    }
    setLaunchingId(game.id);
    try {
      const session = await api.createCasinoSession(game.type, game.provider_id);
      if (session.url) window.open(session.url, "_blank");
    } catch {
      addToast({ type: "error", title: "Failed to launch game" });
    } finally {
      setLaunchingId(null);
    }
  }

  return (
    <div className="max-w-7xl mx-auto px-3 py-4 space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-lg font-bold text-white">Virtual Sports</h1>
        <p className="text-xs text-gray-500">
          Bet on simulated sporting events running 24/7
        </p>
      </div>

      {/* Banner */}
      <div className="bg-gradient-to-r from-blue-700 to-purple-800 rounded-2xl p-5">
        <h2 className="text-xl font-bold text-white">Always Live</h2>
        <p className="text-sm text-white/60 mt-1">
          Virtual sports run around the clock with events every few minutes
        </p>
      </div>

      {/* Games Grid */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {VIRTUAL_SPORTS.map((sport) => (
          <button
            key={sport.id}
            onClick={() => launchGame(sport)}
            disabled={launchingId === sport.id}
            className="text-left group"
          >
            <div className={`bg-gradient-to-br ${sport.color} rounded-xl p-5 h-44 flex flex-col justify-between transition group-hover:scale-[1.02] group-hover:shadow-lg`}>
              <div className="flex items-start justify-between">
                <div>
                  <span className="inline-flex items-center gap-1 bg-black/30 text-white text-[10px] px-1.5 py-0.5 rounded">
                    <span className="w-1 h-1 bg-profit rounded-full animate-pulse" />
                    24/7
                  </span>
                </div>
                <div className="text-3xl font-bold text-white/20">
                  {sport.icon}
                </div>
              </div>
              <div>
                <h3 className="text-base font-bold text-white">{sport.name}</h3>
                <p className="text-xs text-white/60 mt-0.5">{sport.description}</p>
              </div>
            </div>
            <div className="mt-2 px-1">
              <p className="text-[10px] text-gray-500">{sport.provider_name}</p>
            </div>
          </button>
        ))}
      </div>

      {/* Info */}
      <div className="bg-surface rounded-xl border border-gray-800 p-5">
        <h2 className="text-sm font-bold text-white mb-3">How Virtual Sports Work</h2>
        <div className="space-y-2 text-xs text-gray-400">
          <p>Virtual sports use Random Number Generators (RNG) to simulate realistic sporting events.</p>
          <p>Events run every 2-5 minutes, 24 hours a day, 7 days a week.</p>
          <p>Each event is independent with outcomes determined by certified fair algorithms.</p>
          <p>Bet types include match winner, over/under, correct score, and more.</p>
        </div>
      </div>
    </div>
  );
}
