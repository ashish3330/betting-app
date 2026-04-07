"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api, Sport } from "@/lib/api";

const DEFAULT_SPORTS: (Sport & { icon: string; color: string })[] = [
  { id: "cricket", name: "Cricket", slug: "cricket", active_events: 0, icon: "CR", color: "from-green-600 to-emerald-700" },
  { id: "football", name: "Football", slug: "football", active_events: 0, icon: "FB", color: "from-blue-600 to-indigo-700" },
  { id: "tennis", name: "Tennis", slug: "tennis", active_events: 0, icon: "TN", color: "from-yellow-500 to-amber-600" },
  { id: "horse-racing", name: "Horse Racing", slug: "horse-racing", active_events: 0, icon: "HR", color: "from-amber-600 to-orange-700" },
  { id: "kabaddi", name: "Kabaddi", slug: "kabaddi", active_events: 0, icon: "KB", color: "from-red-600 to-rose-700" },
  { id: "basketball", name: "Basketball", slug: "basketball", active_events: 0, icon: "BK", color: "from-orange-500 to-red-600" },
  { id: "table-tennis", name: "Table Tennis", slug: "table-tennis", active_events: 0, icon: "TT", color: "from-cyan-500 to-blue-600" },
  { id: "volleyball", name: "Volleyball", slug: "volleyball", active_events: 0, icon: "VB", color: "from-purple-500 to-violet-600" },
  { id: "esports", name: "Esports", slug: "esports", active_events: 0, icon: "ES", color: "from-fuchsia-600 to-pink-700" },
  { id: "badminton", name: "Badminton", slug: "badminton", active_events: 0, icon: "BD", color: "from-teal-500 to-green-600" },
  { id: "ice-hockey", name: "Ice Hockey", slug: "ice-hockey", active_events: 0, icon: "IH", color: "from-sky-500 to-blue-700" },
  { id: "baseball", name: "Baseball", slug: "baseball", active_events: 0, icon: "BB", color: "from-red-500 to-rose-700" },
];

export default function SportsPage() {
  const [sports, setSports] = useState<Sport[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    loadSports();
  }, []);

  async function loadSports() {
    try {
      const data = await api.fetchSports();
      setSports(Array.isArray(data) ? data : []);
    } catch {
      // Use defaults
    } finally {
      setLoading(false);
    }
  }

  const displaySports = sports.length > 0 ? sports : DEFAULT_SPORTS;

  return (
    <div className="max-w-7xl mx-auto px-3 py-4 space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-lg font-bold text-white">All Sports</h1>
        <p className="text-xs text-gray-500">
          Choose a sport to view live and upcoming competitions
        </p>
      </div>

      {/* Sports Grid */}
      {loading ? (
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
          {Array.from({ length: 8 }).map((_, i) => (
            <div
              key={i}
              className="bg-surface rounded-xl border border-gray-800 h-36 animate-pulse"
            />
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
          {displaySports.map((sport) => {
            const defaultSport = DEFAULT_SPORTS.find((d) => d.slug === sport.slug);
            const gradientClass = defaultSport?.color || "from-gray-700 to-gray-800";
            const iconText = defaultSport?.icon || sport.name.slice(0, 2).toUpperCase();

            return (
              <Link
                key={sport.id}
                href={`/sports/${sport.slug}`}
                className="group"
              >
                <div
                  className={`bg-gradient-to-br ${gradientClass} rounded-xl p-4 h-36 flex flex-col justify-between transition group-hover:scale-[1.02] group-hover:shadow-lg`}
                >
                  <div className="text-3xl font-bold text-white/20">
                    {iconText}
                  </div>
                  <div>
                    <h3 className="text-sm font-bold text-white">
                      {sport.name}
                    </h3>
                    {sport.active_events > 0 && (
                      <span className="inline-flex items-center gap-1 text-[10px] text-white/70 mt-1">
                        <span className="w-1.5 h-1.5 bg-profit rounded-full animate-pulse" />
                        {sport.active_events} live
                      </span>
                    )}
                  </div>
                </div>
              </Link>
            );
          })}
        </div>
      )}
    </div>
  );
}
