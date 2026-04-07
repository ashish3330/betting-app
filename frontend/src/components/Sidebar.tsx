"use client";

import { useState, useEffect, useCallback } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { api, Sport, Competition, SportEvent } from "@/lib/api";

const SPORT_ICONS: Record<string, string> = {
  cricket: "\uD83C\uDFCF",
  football: "\u26BD",
  tennis: "\uD83C\uDFBE",
  "horse-racing": "\uD83C\uDFC7",
  kabaddi: "\uD83E\uDD3C",
  basketball: "\uD83C\uDFC0",
  volleyball: "\uD83C\uDFD0",
  "table-tennis": "\uD83C\uDFD3",
  badminton: "\uD83C\uDFF8",
  baseball: "\u26BE",
  boxing: "\uD83E\uDD4A",
  esports: "\uD83C\uDFAE",
  politics: "\uD83C\uDFDB\uFE0F",
};

interface SidebarProps {
  isOpen?: boolean;
  onClose?: () => void;
}

interface CompetitionWithEvents extends Competition {
  events: SportEvent[];
  isExpanded: boolean;
  isLoading: boolean;
}

interface SportNode extends Sport {
  competitions: CompetitionWithEvents[];
  isExpanded: boolean;
  isLoading: boolean;
}

export default function Sidebar({ isOpen = true, onClose }: SidebarProps) {
  const pathname = usePathname();
  const [sports, setSports] = useState<SportNode[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    async function fetchSports() {
      try {
        const data = await api.fetchSports();
        if (!cancelled) {
          setSports(
            data.map((s) => ({
              ...s,
              competitions: [],
              isExpanded: false,
              isLoading: false,
            }))
          );
        }
      } catch {
        // silently fail
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    fetchSports();
    return () => {
      cancelled = true;
    };
  }, []);

  const toggleSport = useCallback(async (sportSlug: string) => {
    setSports((prev) =>
      prev.map((s) => {
        if (s.slug !== sportSlug) return s;
        if (s.isExpanded) return { ...s, isExpanded: false };
        if (s.competitions.length > 0) return { ...s, isExpanded: true };
        return { ...s, isExpanded: true, isLoading: true };
      })
    );

    // Fetch competitions if not already loaded
    setSports((prev) => {
      const sport = prev.find((s) => s.slug === sportSlug);
      if (sport && sport.competitions.length === 0 && sport.isExpanded) {
        api
          .fetchCompetitions(sportSlug)
          .then((comps) => {
            setSports((current) =>
              current.map((s) =>
                s.slug === sportSlug
                  ? {
                      ...s,
                      isLoading: false,
                      competitions: comps.map((c) => ({
                        ...c,
                        events: [],
                        isExpanded: false,
                        isLoading: false,
                      })),
                    }
                  : s
              )
            );
          })
          .catch(() => {
            setSports((current) =>
              current.map((s) =>
                s.slug === sportSlug ? { ...s, isLoading: false } : s
              )
            );
          });
      }
      return prev;
    });
  }, []);

  const toggleCompetition = useCallback(
    async (sportSlug: string, competitionId: string) => {
      setSports((prev) =>
        prev.map((s) => {
          if (s.slug !== sportSlug) return s;
          return {
            ...s,
            competitions: s.competitions.map((c) => {
              if (c.id !== competitionId) return c;
              if (c.isExpanded) return { ...c, isExpanded: false };
              if (c.events.length > 0) return { ...c, isExpanded: true };
              return { ...c, isExpanded: true, isLoading: true };
            }),
          };
        })
      );

      // Fetch events if not loaded
      const sport = sports.find((s) => s.slug === sportSlug);
      const comp = sport?.competitions.find((c) => c.id === competitionId);
      if (comp && comp.events.length === 0) {
        try {
          const events = await api.fetchEvents(competitionId);
          setSports((current) =>
            current.map((s) => {
              if (s.slug !== sportSlug) return s;
              return {
                ...s,
                competitions: s.competitions.map((c) =>
                  c.id === competitionId
                    ? { ...c, events, isLoading: false }
                    : c
                ),
              };
            })
          );
        } catch {
          setSports((current) =>
            current.map((s) => {
              if (s.slug !== sportSlug) return s;
              return {
                ...s,
                competitions: s.competitions.map((c) =>
                  c.id === competitionId ? { ...c, isLoading: false } : c
                ),
              };
            })
          );
        }
      }
    },
    [sports]
  );

  const getEventUrl = (event: SportEvent): string => {
    if (event.market_id) return `/markets/${event.market_id}`;
    return `/markets/${event.id}`;
  };

  return (
    <>
      {/* Mobile overlay */}
      {isOpen && (
        <div
          className="fixed inset-0 bg-black/60 z-40 lg:hidden"
          onClick={onClose}
        />
      )}

      <aside
        className={`fixed lg:sticky top-0 lg:top-[50px] left-0 z-50 lg:z-30 h-screen lg:h-[calc(100vh-50px)] w-[260px] bg-[var(--bg-surface)] border-r border-gray-800/60 overflow-y-auto transition-transform duration-300 ease-in-out ${
          isOpen ? "translate-x-0" : "-translate-x-full lg:translate-x-0"
        }`}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-800/60">
          <h2 className="text-sm font-semibold text-gray-300 uppercase tracking-wider">
            Sports
          </h2>
          {onClose && (
            <button
              onClick={onClose}
              className="lg:hidden p-1 text-gray-500 hover:text-white transition"
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          )}
        </div>

        {/* Loading state */}
        {loading && (
          <div className="px-4 py-6 space-y-3">
            {[1, 2, 3, 4, 5].map((i) => (
              <div key={i} className="h-8 bg-gray-800/40 rounded animate-pulse" />
            ))}
          </div>
        )}

        {/* Sports tree */}
        <nav className="py-2">
          {sports.map((sport) => (
            <div key={sport.id}>
              {/* Sport row */}
              <button
                onClick={() => toggleSport(sport.slug)}
                className={`w-full flex items-center justify-between px-4 py-2.5 text-left hover:bg-white/5 transition-colors group ${
                  sport.isExpanded ? "bg-white/5" : ""
                }`}
              >
                <div className="flex items-center gap-2.5 min-w-0">
                  <span className="text-base flex-shrink-0">
                    {SPORT_ICONS[sport.slug] || "\uD83C\uDFC6"}
                  </span>
                  <span
                    className={`text-sm font-medium truncate ${
                      sport.isExpanded
                        ? "text-lotus"
                        : "text-gray-300 group-hover:text-white"
                    }`}
                  >
                    {sport.name}
                  </span>
                </div>
                <div className="flex items-center gap-2 flex-shrink-0">
                  {sport.active_events > 0 && (
                    <span className="text-[10px] font-bold bg-red-500/20 text-red-400 px-1.5 py-0.5 rounded-full min-w-[20px] text-center">
                      {sport.active_events}
                    </span>
                  )}
                  <svg
                    className={`w-3.5 h-3.5 text-gray-500 transition-transform duration-200 ${
                      sport.isExpanded ? "rotate-180" : ""
                    }`}
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                  </svg>
                </div>
              </button>

              {/* Competitions */}
              <div
                className={`overflow-hidden transition-all duration-300 ease-in-out ${
                  sport.isExpanded ? "max-h-[2000px] opacity-100" : "max-h-0 opacity-0"
                }`}
              >
                {sport.isLoading && (
                  <div className="pl-10 pr-4 py-2">
                    <div className="h-5 bg-gray-800/30 rounded animate-pulse" />
                  </div>
                )}

                {sport.competitions.map((comp) => (
                  <div key={comp.id}>
                    {/* Competition row */}
                    <button
                      onClick={() => toggleCompetition(sport.slug, comp.id)}
                      className={`w-full flex items-center justify-between pl-10 pr-4 py-2 text-left hover:bg-white/5 transition-colors ${
                        comp.isExpanded ? "bg-white/[0.03]" : ""
                      }`}
                    >
                      <span
                        className={`text-xs truncate ${
                          comp.isExpanded
                            ? "text-lotus font-medium"
                            : "text-gray-400 hover:text-white"
                        }`}
                      >
                        {comp.name}
                      </span>
                      <div className="flex items-center gap-1.5 flex-shrink-0">
                        <span className="text-[10px] text-gray-400">
                          {comp.events_count}
                        </span>
                        <svg
                          className={`w-3 h-3 text-gray-400 transition-transform duration-200 ${
                            comp.isExpanded ? "rotate-180" : ""
                          }`}
                          fill="none"
                          stroke="currentColor"
                          viewBox="0 0 24 24"
                        >
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                        </svg>
                      </div>
                    </button>

                    {/* Events */}
                    <div
                      className={`overflow-hidden transition-all duration-300 ease-in-out ${
                        comp.isExpanded ? "max-h-[2000px] opacity-100" : "max-h-0 opacity-0"
                      }`}
                    >
                      {comp.isLoading && (
                        <div className="pl-14 pr-4 py-1.5">
                          <div className="h-4 bg-gray-800/20 rounded animate-pulse" />
                        </div>
                      )}

                      {comp.events.map((event) => {
                        const eventUrl = getEventUrl(event);
                        const isActive = pathname === eventUrl;
                        return (
                          <Link
                            key={event.id}
                            href={eventUrl}
                            onClick={onClose}
                            className={`flex items-center gap-2 pl-14 pr-4 py-1.5 text-left transition-colors ${
                              isActive
                                ? "bg-lotus/10 border-l-2 border-lotus"
                                : "hover:bg-white/5"
                            }`}
                          >
                            {event.in_play && (
                              <span className="flex-shrink-0 w-1.5 h-1.5 rounded-full bg-green-500 animate-live-pulse" />
                            )}
                            <span
                              className={`text-[11px] leading-tight truncate ${
                                isActive
                                  ? "text-lotus font-medium"
                                  : "text-gray-500 hover:text-gray-300"
                              }`}
                            >
                              {event.name}
                            </span>
                            {event.in_play && (
                              <span className="flex-shrink-0 text-[9px] font-bold text-green-400 bg-green-400/10 px-1 py-0.5 rounded uppercase">
                                Live
                              </span>
                            )}
                          </Link>
                        );
                      })}
                    </div>
                  </div>
                ))}

                {!sport.isLoading && sport.competitions.length === 0 && (
                  <div className="pl-10 pr-4 py-2">
                    <span className="text-[11px] text-gray-400 italic">
                      No competitions available
                    </span>
                  </div>
                )}
              </div>
            </div>
          ))}

          {!loading && sports.length === 0 && (
            <div className="px-4 py-8 text-center">
              <span className="text-sm text-gray-400">No sports available</span>
            </div>
          )}
        </nav>
      </aside>
    </>
  );
}
