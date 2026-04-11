"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api, Sport } from "@/lib/api";

type SportMeta = {
  icon: React.ReactNode;
  color: string;
};

const ICON_PROPS = {
  width: 28,
  height: 28,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.5,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
};

const SPORT_META: Record<string, SportMeta> = {
  cricket: {
    icon: (
      <svg {...ICON_PROPS}>
        <circle cx="7" cy="17" r="2.5" />
        <path d="M9 15L19 5" />
        <path d="M15 3l6 6-2 2-6-6z" />
      </svg>
    ),
    color: "from-green-600 to-emerald-700",
  },
  football: {
    icon: (
      <svg {...ICON_PROPS}>
        <circle cx="12" cy="12" r="9" />
        <path d="M12 4l3 4-1.5 5h-3L9 8z" />
        <path d="M12 4v4" />
        <path d="M4.5 10l3.5 1M16 11l3.5-1M7 19l2-3M15 16l2 3" />
      </svg>
    ),
    color: "from-blue-600 to-indigo-700",
  },
  tennis: {
    icon: (
      <svg {...ICON_PROPS}>
        <circle cx="12" cy="12" r="9" />
        <path d="M4 6c4 3 4 9 0 12" />
        <path d="M20 6c-4 3-4 9 0 12" />
      </svg>
    ),
    color: "from-yellow-500 to-amber-600",
  },
  "horse-racing": {
    icon: (
      <svg {...ICON_PROPS}>
        <path d="M5 20l1-5 3-2 2 3 4-1 3 5" />
        <path d="M15 9l3-3 2 1-1 3-3 1z" />
        <circle cx="18" cy="7" r="0.5" fill="currentColor" />
      </svg>
    ),
    color: "from-amber-600 to-orange-700",
  },
  kabaddi: {
    icon: (
      <svg {...ICON_PROPS}>
        <circle cx="12" cy="5" r="2" />
        <path d="M12 7v6l-3 4M12 13l3 4M9 10l-3 1M15 10l3 1" />
      </svg>
    ),
    color: "from-red-600 to-rose-700",
  },
  basketball: {
    icon: (
      <svg {...ICON_PROPS}>
        <circle cx="12" cy="12" r="9" />
        <path d="M3 12h18M12 3v18" />
        <path d="M5.5 5.5c3 3 3 10 0 13M18.5 5.5c-3 3-3 10 0 13" />
      </svg>
    ),
    color: "from-orange-500 to-red-600",
  },
  "table-tennis": {
    icon: (
      <svg {...ICON_PROPS}>
        <circle cx="10" cy="10" r="6" />
        <path d="M14 14l5 5" />
        <circle cx="19" cy="6" r="1.2" />
      </svg>
    ),
    color: "from-cyan-500 to-blue-600",
  },
  volleyball: {
    icon: (
      <svg {...ICON_PROPS}>
        <circle cx="12" cy="12" r="9" />
        <path d="M12 3c3 3 4 7 3 11M12 3c-3 3-4 7-3 11M3 12c3-2 7-2 11 1M21 12c-3-2-7-2-11 1" />
      </svg>
    ),
    color: "from-purple-500 to-violet-600",
  },
  esports: {
    icon: (
      <svg {...ICON_PROPS}>
        <rect x="3" y="8" width="18" height="10" rx="3" />
        <path d="M7 13h2M8 12v2M15 12.5h.01M17 13.5h.01" />
      </svg>
    ),
    color: "from-fuchsia-600 to-pink-700",
  },
  badminton: {
    icon: (
      <svg {...ICON_PROPS}>
        <path d="M4 20l6-6" />
        <circle cx="12" cy="12" r="3" />
        <path d="M14 10l4-4 2 2-4 4M16 8l-1-1M18 10l-1-1" />
      </svg>
    ),
    color: "from-teal-500 to-green-600",
  },
  "ice-hockey": {
    icon: (
      <svg {...ICON_PROPS}>
        <ellipse cx="12" cy="17" rx="5" ry="1.5" />
        <path d="M7 17V9l3-4 4 1 3 4v7" />
      </svg>
    ),
    color: "from-sky-500 to-blue-700",
  },
  ice_hockey: {
    icon: (
      <svg {...ICON_PROPS}>
        <ellipse cx="12" cy="17" rx="5" ry="1.5" />
        <path d="M7 17V9l3-4 4 1 3 4v7" />
      </svg>
    ),
    color: "from-sky-500 to-blue-700",
  },
  baseball: {
    icon: (
      <svg {...ICON_PROPS}>
        <circle cx="12" cy="12" r="9" />
        <path d="M5.5 6.5c2 2 3 5 3 8M18.5 6.5c-2 2-3 5-3 8M7 9l-1 .5M8 12l-1.5.3M17 9l1 .5M16 12l1.5.3" />
      </svg>
    ),
    color: "from-red-500 to-rose-700",
  },
  boxing: {
    icon: (
      <svg {...ICON_PROPS}>
        <path d="M7 8h6l2 3v5a2 2 0 01-2 2H9a2 2 0 01-2-2V8z" />
        <path d="M13 8V6a2 2 0 00-2-2H9a2 2 0 00-2 2v2" />
      </svg>
    ),
    color: "from-red-700 to-rose-900",
  },
};

const DEFAULT_SPORTS: Sport[] = [
  { id: "cricket", name: "Cricket", slug: "cricket", active_events: 0 },
  { id: "football", name: "Football", slug: "football", active_events: 0 },
  { id: "tennis", name: "Tennis", slug: "tennis", active_events: 0 },
  { id: "horse-racing", name: "Horse Racing", slug: "horse-racing", active_events: 0 },
  { id: "kabaddi", name: "Kabaddi", slug: "kabaddi", active_events: 0 },
  { id: "basketball", name: "Basketball", slug: "basketball", active_events: 0 },
  { id: "table-tennis", name: "Table Tennis", slug: "table-tennis", active_events: 0 },
  { id: "volleyball", name: "Volleyball", slug: "volleyball", active_events: 0 },
  { id: "esports", name: "Esports", slug: "esports", active_events: 0 },
  { id: "badminton", name: "Badminton", slug: "badminton", active_events: 0 },
  { id: "ice-hockey", name: "Ice Hockey", slug: "ice-hockey", active_events: 0 },
  { id: "baseball", name: "Baseball", slug: "baseball", active_events: 0 },
];

export default function SportsPage() {
  const [sports, setSports] = useState<Sport[]>([]);
  const [liveCounts, setLiveCounts] = useState<Record<string, number>>({});
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      let fetched: Sport[] = [];
      try {
        const data = await api.fetchSports();
        fetched = Array.isArray(data) ? data : [];
      } catch {
        fetched = [];
      }

      const list = fetched.length > 0 ? fetched : DEFAULT_SPORTS;
      if (!cancelled) {
        setSports(list);
        setLoading(false);
      }

      // Fetch live counts per sport in parallel
      const results = await Promise.allSettled(
        list.map((s) =>
          api.fetchEventsBySport(s.slug.replace(/-/g, "_")).catch(() => [])
        )
      );
      if (cancelled) return;

      const counts: Record<string, number> = {};
      results.forEach((r, i) => {
        if (r.status === "fulfilled" && Array.isArray(r.value)) {
          counts[list[i].slug] = r.value.filter((e) => e.in_play).length;
        } else {
          counts[list[i].slug] = 0;
        }
      });
      setLiveCounts(counts);
    }

    load();
    return () => {
      cancelled = true;
    };
  }, []);

  const displaySports = sports.length > 0 ? sports : DEFAULT_SPORTS;

  // Merge live counts with API-reported active_events (prefer live count if > 0)
  const withCounts = displaySports.map((s) => ({
    ...s,
    active_events: liveCounts[s.slug] ?? s.active_events ?? 0,
  }));

  // Sort: in-play first, then alphabetical
  const sorted = [...withCounts].sort((a, b) => {
    const la = a.active_events || 0;
    const lb = b.active_events || 0;
    if (la > 0 && lb === 0) return -1;
    if (lb > 0 && la === 0) return 1;
    if (la !== lb) return lb - la;
    return a.name.localeCompare(b.name);
  });

  const inPlay = sorted.filter((s) => (s.active_events || 0) > 0);
  const others = sorted.filter((s) => (s.active_events || 0) === 0);

  return (
    <div className="max-w-[1200px] mx-auto px-3 py-4 space-y-5">
      {/* Header */}
      <div>
        <h1 className="text-lg font-bold text-white">All Sports</h1>
        <p className="text-xs text-gray-500">
          Choose a sport to view live and upcoming competitions
        </p>
      </div>

      {loading && (
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
          {Array.from({ length: 8 }).map((_, i) => (
            <div
              key={i}
              className="bg-surface rounded-xl border border-gray-800 h-36 animate-pulse"
            />
          ))}
        </div>
      )}

      {!loading && inPlay.length > 0 && (
        <section>
          <div className="flex items-center gap-2 mb-2">
            <span className="w-1.5 h-1.5 bg-green-500 rounded-full animate-pulse" />
            <h2 className="text-[11px] font-bold text-gray-400 uppercase tracking-wider">
              In-Play Now
            </h2>
            <span className="text-[10px] text-gray-600">({inPlay.length})</span>
          </div>
          <SportGrid sports={inPlay} />
        </section>
      )}

      {!loading && others.length > 0 && (
        <section>
          <div className="flex items-center gap-2 mb-2">
            <h2 className="text-[11px] font-bold text-gray-400 uppercase tracking-wider">
              All Sports
            </h2>
            <span className="text-[10px] text-gray-600">({others.length})</span>
          </div>
          <SportGrid sports={others} />
        </section>
      )}
    </div>
  );
}

function SportGrid({ sports }: { sports: Sport[] }) {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
      {sports.map((sport) => {
        const meta =
          SPORT_META[sport.slug] ||
          SPORT_META[sport.slug.replace(/_/g, "-")] ||
          {
            icon: (
              <svg {...ICON_PROPS}>
                <circle cx="12" cy="12" r="9" />
              </svg>
            ),
            color: "from-gray-700 to-gray-800",
          };
        const liveEvents = sport.active_events || 0;

        return (
          <Link key={sport.id} href={`/sports/${sport.slug}`} className="group">
            <div
              className={`relative bg-gradient-to-br ${meta.color} rounded-xl p-4 h-36 flex flex-col justify-between overflow-hidden transition group-hover:scale-[1.02] group-hover:shadow-lg`}
            >
              {/* Decorative icon background */}
              <div className="absolute -right-2 -bottom-2 text-white/10 pointer-events-none">
                <div className="scale-[3]">{meta.icon}</div>
              </div>

              {/* Icon at top */}
              <div className="relative text-white/90">{meta.icon}</div>

              <div className="relative">
                <h3 className="text-sm font-bold text-white">{sport.name}</h3>
                {liveEvents > 0 ? (
                  <span className="inline-flex items-center gap-1 text-[10px] font-semibold text-white bg-red-500/80 px-1.5 py-0.5 rounded mt-1">
                    <span className="w-1 h-1 bg-white rounded-full animate-pulse" />
                    {liveEvents} LIVE
                  </span>
                ) : (
                  <span className="text-[10px] text-white/50 mt-1 block">
                    View events
                  </span>
                )}
              </div>
            </div>
          </Link>
        );
      })}
    </div>
  );
}
