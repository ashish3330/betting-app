"use client";

import Link from "next/link";
import { Market } from "@/lib/api";

const BACK_BG = ["bg-[#72BBEF]", "bg-[#A8D8F0]", "bg-[#BBE4F7]"];
const LAY_BG = ["bg-[#FAA9BA]", "bg-[#F7C3CF]", "bg-[#FACBD7]"];

export default function MarketCard({ market }: { market: Market }) {
  const isLive = market.in_play || market.status === "in_play";
  const isSuspended = market.status === "suspended";

  return (
    <Link href={`/markets/${market.id}`} className="block">
      <div className={`bg-surface border overflow-hidden transition ${
        isSuspended ? "border-yellow-900/30 opacity-60" : "border-gray-800/40 hover:border-gray-600"
      }`}>
        {/* Match Header — name + meta */}
        <div className="px-2.5 py-1.5 border-b border-gray-800/30">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-1.5 min-w-0 flex-1">
              {isLive && (
                <span className="flex items-center gap-1 text-[10px] text-green-400 font-bold flex-shrink-0">
                  <span className="w-1.5 h-1.5 bg-green-500 rounded-full animate-pulse" />
                  LIVE
                </span>
              )}
              <span className="text-[10px] text-gray-500 truncate">{market.competition || market.sport?.toUpperCase()}</span>
            </div>
            <div className="flex items-center gap-2 flex-shrink-0 text-[9px] text-gray-500 tabular-nums">
              {(market.total_matched || 0) > 0 && <span className="font-mono">₹{fmt(market.total_matched || 0)}</span>}
              <span>{isLive ? "In-Play" : new Date(market.start_time).toLocaleString("en-IN", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })}</span>
            </div>
          </div>
          {/* EVENT NAME — blinks for live matches like playzone9 */}
          <h3 className={`text-[13px] font-semibold leading-tight mt-0.5 truncate ${
            isLive ? "text-white animate-pulse" : "text-white"
          }`}>
            {market.event_name || market.name || "Market"}
          </h3>
        </div>

        {/* Odds Table */}
        <div className="relative">
          {isSuspended && (
            <div className="absolute inset-0 bg-black/50 z-10 flex items-center justify-center">
              <span className="text-[10px] font-bold text-yellow-400 tracking-widest">SUSPENDED</span>
            </div>
          )}

          {/* Column headers: Match Odds label + Back/Lay */}
          <div className="flex items-center border-b border-gray-800/20">
            <div className="flex-1 px-2.5">
              <span className="text-[9px] text-gray-500 font-medium">Match Odds</span>
            </div>
            <div className="flex gap-px w-[120px] sm:w-[360px] flex-shrink-0">
              <div className="flex-1 text-center text-[8px] font-bold text-[#72BBEF] py-0.5 hidden sm:block">Back</div>
              <div className="flex-1 text-center text-[8px] font-bold text-[#72BBEF] py-0.5 hidden sm:block">Back</div>
              <div className="flex-1 text-center text-[8px] font-bold text-[#72BBEF] py-0.5">Back</div>
              <div className="flex-1 text-center text-[8px] font-bold text-[#FAA9BA] py-0.5">Lay</div>
              <div className="flex-1 text-center text-[8px] font-bold text-[#FAA9BA] py-0.5 hidden sm:block">Lay</div>
              <div className="flex-1 text-center text-[8px] font-bold text-[#FAA9BA] py-0.5 hidden sm:block">Lay</div>
            </div>
          </div>

          {market.runners?.slice(0, 3).map((r) => {
            const backs = r.back_prices || [];
            const lays = r.lay_prices || [];
            const b3 = backs[2] || { price: 0, size: 0 };
            const b2 = backs[1] || { price: 0, size: 0 };
            const b1 = backs[0] || { price: r.back_price || 0, size: r.back_size || 0 };
            const l1 = lays[0] || { price: r.lay_price || 0, size: r.lay_size || 0 };
            const l2 = lays[1] || { price: 0, size: 0 };
            const l3 = lays[2] || { price: 0, size: 0 };

            return (
              <div key={r.id || r.selection_id || r.name}
                className="flex items-center border-b border-gray-800/15 last:border-0">
                <div className="flex-1 min-w-0 px-2.5 py-1">
                  <span className="text-[11px] text-gray-300 truncate block">{r.name}</span>
                </div>
                <div className="flex gap-px w-[120px] sm:w-[360px] flex-shrink-0">
                  <Cell p={b3.price} s={b3.size} bg={BACK_BG[2]} hide />
                  <Cell p={b2.price} s={b2.size} bg={BACK_BG[1]} hide />
                  <Cell p={b1.price} s={b1.size} bg={BACK_BG[0]} />
                  <Cell p={l1.price} s={l1.size} bg={LAY_BG[0]} />
                  <Cell p={l2.price} s={l2.size} bg={LAY_BG[1]} hide />
                  <Cell p={l3.price} s={l3.size} bg={LAY_BG[2]} hide />
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </Link>
  );
}

function Cell({ p, s, bg, hide }: { p: number; s: number; bg: string; hide?: boolean }) {
  return (
    <div className={`flex-1 ${bg} flex flex-col items-center justify-center py-1.5 min-h-[34px] ${hide ? "hidden sm:flex" : ""}`}>
      {p > 0 ? (
        <>
          <span className="text-[11px] font-bold text-black leading-none tabular-nums">{p.toFixed(2)}</span>
          {s > 0 && <span className="text-[8px] text-black/35 leading-none mt-px tabular-nums">{fmt(s)}</span>}
        </>
      ) : (
        <span className="text-[9px] text-black/20">-</span>
      )}
    </div>
  );
}

function fmt(n: number): string {
  if (n >= 10000000) return `${(n / 10000000).toFixed(1)}Cr`;
  if (n >= 100000) return `${(n / 100000).toFixed(1)}L`;
  if (n >= 1000) return `${(n / 1000).toFixed(0)}K`;
  return n.toFixed(0);
}
