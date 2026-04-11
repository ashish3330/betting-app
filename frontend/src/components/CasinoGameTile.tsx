"use client";

import { CasinoGameItem, CATEGORY_GRADIENT, GameCategory } from "@/lib/casino-games";

interface Props {
  game: CasinoGameItem;
  launching?: boolean;
  onLaunch: (game: CasinoGameItem) => void;
}

/**
 * Inline SVG glyphs used inside each casino tile. One per category.
 * Kept small and monochromatic so they render nicely on the gradient tile.
 */
function CategoryGlyph({ category }: { category: GameCategory }) {
  const common = "w-8 h-8 text-white/90 drop-shadow-lg";
  switch (category) {
    case "live_casino":
      // roulette-ish circle with inner dot
      return (
        <svg className={common} viewBox="0 0 32 32" fill="none" stroke="currentColor" strokeWidth="1.5">
          <circle cx="16" cy="16" r="12" />
          <circle cx="16" cy="16" r="6" />
          <circle cx="16" cy="16" r="1.5" fill="currentColor" />
          <line x1="16" y1="4" x2="16" y2="10" />
          <line x1="16" y1="22" x2="16" y2="28" />
          <line x1="4" y1="16" x2="10" y2="16" />
          <line x1="22" y1="16" x2="28" y2="16" />
        </svg>
      );
    case "card":
      // playing-card outline with suit marks
      return (
        <svg className={common} viewBox="0 0 32 32" fill="none" stroke="currentColor" strokeWidth="1.5">
          <rect x="7" y="4" width="14" height="20" rx="2" />
          <rect x="11" y="8" width="14" height="20" rx="2" fill="currentColor" fillOpacity="0.15" />
          <path d="M14 12l2 2 2-2-2-2z" fill="currentColor" />
        </svg>
      );
    case "slots":
      // 3x3 grid
      return (
        <svg className={common} viewBox="0 0 32 32" fill="none" stroke="currentColor" strokeWidth="1.5">
          <rect x="4" y="4" width="7" height="7" rx="1" fill="currentColor" fillOpacity="0.3" />
          <rect x="13" y="4" width="7" height="7" rx="1" />
          <rect x="22" y="4" width="7" height="7" rx="1" fill="currentColor" fillOpacity="0.3" />
          <rect x="4" y="13" width="7" height="7" rx="1" />
          <rect x="13" y="13" width="7" height="7" rx="1" fill="currentColor" fillOpacity="0.3" />
          <rect x="22" y="13" width="7" height="7" rx="1" />
          <rect x="4" y="22" width="7" height="7" rx="1" fill="currentColor" fillOpacity="0.3" />
          <rect x="13" y="22" width="7" height="7" rx="1" />
          <rect x="22" y="22" width="7" height="7" rx="1" fill="currentColor" fillOpacity="0.3" />
        </svg>
      );
    case "crash":
      // rocket / upward arrow
      return (
        <svg className={common} viewBox="0 0 32 32" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round">
          <path d="M6 26L22 10" />
          <path d="M14 8h8v8" />
          <path d="M6 22l2 4 4-2" />
        </svg>
      );
    case "virtual_sports":
      // trophy/ball combo
      return (
        <svg className={common} viewBox="0 0 32 32" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <circle cx="16" cy="14" r="9" />
          <path d="M16 5v18M7 14h18" />
          <path d="M9 9l14 10M23 9L9 19" />
        </svg>
      );
    default:
      return null;
  }
}

export function CasinoGameTile({ game, launching, onLaunch }: Props) {
  const gradient = CATEGORY_GRADIENT[game.category] ?? "from-gray-800 to-gray-900";

  return (
    <button
      onClick={() => onLaunch(game)}
      disabled={launching}
      className="text-left group"
    >
      <div
        className={`relative overflow-hidden rounded-lg border border-gray-800 h-36 sm:h-40 p-3 sm:p-4 flex flex-col justify-between bg-gradient-to-br ${gradient} transition-all duration-200 group-hover:scale-[1.03] group-hover:brightness-110 group-hover:border-white/20 group-hover:shadow-lg group-hover:shadow-black/40`}
      >
        {/* Subtle radial glow */}
        <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_at_top_right,_rgba(255,255,255,0.12),_transparent_60%)]" />

        {/* Top row: provider + badges */}
        <div className="relative flex items-start justify-between">
          <span className="bg-black/40 backdrop-blur text-white/80 text-[9px] sm:text-[10px] font-semibold px-2 py-0.5 rounded uppercase tracking-wider">
            {game.provider_name}
          </span>
          <div className="flex flex-col items-end gap-1">
            {game.is_live && (
              <span className="bg-red-500/30 backdrop-blur text-red-100 text-[9px] font-bold px-1.5 py-0.5 rounded flex items-center gap-1 border border-red-400/40">
                <span className="w-1 h-1 bg-red-300 rounded-full animate-pulse" />
                LIVE
              </span>
            )}
            {game.badge === "NEW" && (
              <span className="bg-emerald-500/30 backdrop-blur text-emerald-100 text-[9px] font-bold px-1.5 py-0.5 rounded border border-emerald-400/40">
                NEW
              </span>
            )}
            {game.badge === "HOT" && (
              <span className="bg-orange-500/30 backdrop-blur text-orange-100 text-[9px] font-bold px-1.5 py-0.5 rounded border border-orange-400/40">
                HOT
              </span>
            )}
          </div>
        </div>

        {/* Center glyph + name */}
        <div className="relative flex-1 flex flex-col items-center justify-center gap-2">
          <CategoryGlyph category={game.category} />
          <div className="text-[11px] sm:text-xs font-semibold text-white/95 text-center px-1 truncate max-w-full">
            {game.name}
          </div>
        </div>

        {launching && (
          <div className="absolute inset-0 flex items-center justify-center bg-black/60 rounded-lg">
            <div className="w-5 h-5 border-2 border-white/20 border-t-white rounded-full animate-spin" />
          </div>
        )}
      </div>

      {/* Below-card info */}
      <div className="mt-2 px-1">
        <h3 className="text-sm font-medium text-white truncate group-hover:text-gray-300 transition-colors">
          {game.name}
        </h3>
        <div className="flex items-center justify-between mt-0.5">
          <p className="text-[10px] text-gray-500">{game.provider_name}</p>
          {game.min_bet && <p className="text-[10px] text-gray-500">Min: {game.min_bet}</p>}
        </div>
      </div>
    </button>
  );
}
