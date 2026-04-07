"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { Market, api } from "@/lib/api";

// The production exchange table: one row per match, columns for each runner's back/lay
// Layout: [Match Name + Date] [icons] [Runner1 Back|Lay] [Runner2 Back|Lay] [Runner3 Back|Lay]

interface Props {
  markets: Market[];
  title?: string;
  showLiveBadge?: boolean;
  showPositions?: boolean;
}

// Positions map: selectionId (string) -> P&L number
type PositionsMap = Record<string, Record<string, number>>;

export default function MarketTable({ markets, title, showLiveBadge = true, showPositions = false }: Props) {
  const [positions, setPositions] = useState<PositionsMap>({});

  useEffect(() => {
    if (!showPositions || markets.length === 0) return;
    let cancelled = false;

    async function fetchPositions() {
      const result: PositionsMap = {};
      await Promise.all(
        markets.map(async (m) => {
          try {
            const pos = await api.getPositions(m.id);
            // Only store if there are actual positions
            const hasPositions = Object.values(pos).some((v) => v !== 0);
            if (hasPositions) {
              result[m.id] = pos;
            }
          } catch {
            // Not logged in or no positions — ignore
          }
        })
      );
      if (!cancelled) setPositions(result);
    }

    fetchPositions();
    // Refresh positions every 15 seconds
    const interval = setInterval(fetchPositions, 5000);
    return () => { cancelled = true; clearInterval(interval); };
  }, [showPositions, markets]);

  if (markets.length === 0) return null;

  return (
    <div className={`bg-surface border border-gray-800/40 overflow-hidden ${
      showLiveBadge && markets.some(m => m.in_play || m.status === "in_play") ? "border-l-2 border-l-green-500" : ""
    }`}>
      {/* Table Header — desktop only */}
      <div className="hidden sm:flex items-center bg-white/[0.03] border-b border-gray-800/30 px-2 py-1">
        <div className="flex-1 min-w-0">
          <span className="text-[11px] font-bold text-gray-400 uppercase tracking-wider">{title || "Game"}</span>
        </div>
        <div className="flex gap-px w-[360px] flex-shrink-0">
          <div className="flex-1 text-center"><span className="text-[10px] font-bold text-gray-400">1</span></div>
          <div className="flex-1 text-center"><span className="text-[10px] font-bold text-gray-400">X</span></div>
          <div className="flex-1 text-center"><span className="text-[10px] font-bold text-gray-400">2</span></div>
        </div>
      </div>

      {/* Sub-header: Back/Lay labels — desktop only */}
      <div className="hidden sm:flex items-center border-b border-gray-800/20 px-2">
        <div className="flex-1" />
        <div className="flex gap-px w-[360px] flex-shrink-0">
          <div className="flex-1 flex gap-px">
            <span className="flex-1 text-center text-[8px] font-bold text-[#72BBEF] py-0.5 bg-[#72BBEF]/10">Back</span>
            <span className="flex-1 text-center text-[8px] font-bold text-[#FAA9BA] py-0.5 bg-[#FAA9BA]/10">Lay</span>
          </div>
          <div className="flex-1 flex gap-px">
            <span className="flex-1 text-center text-[8px] font-bold text-[#72BBEF] py-0.5 bg-[#72BBEF]/10">Back</span>
            <span className="flex-1 text-center text-[8px] font-bold text-[#FAA9BA] py-0.5 bg-[#FAA9BA]/10">Lay</span>
          </div>
          <div className="flex-1 flex gap-px">
            <span className="flex-1 text-center text-[8px] font-bold text-[#72BBEF] py-0.5 bg-[#72BBEF]/10">Back</span>
            <span className="flex-1 text-center text-[8px] font-bold text-[#FAA9BA] py-0.5 bg-[#FAA9BA]/10">Lay</span>
          </div>
        </div>
      </div>

      {/* Mobile header — simple title bar */}
      <div className="sm:hidden bg-white/[0.03] border-b border-gray-800/30 px-2 py-1">
        <span className="text-[11px] font-bold text-gray-400 uppercase tracking-wider">{title || "Game"}</span>
      </div>

      {/* Rows — one per match */}
      {markets.map((m) => (
        <MarketRow key={m.id} market={m} showLiveBadge={showLiveBadge} positions={positions[m.id]} />
      ))}
    </div>
  );
}

function MarketRow({ market, showLiveBadge, positions }: { market: Market; showLiveBadge: boolean; positions?: Record<string, number> }) {
  const isLive = market.in_play || market.status === "in_play";
  const isSuspended = market.status === "suspended";
  const runners = market.runners || [];
  const r1 = runners[0]; // Home / Team 1
  const r2 = runners.length > 2 ? runners[2] : runners[1]; // Away / Team 2
  const rX = runners.length > 2 ? runners[1] : null; // Draw (if 3 runners)
  const matchName = market.event_name || market.name;
  const marketHref = `/markets/${market.id}`;

  // Get P&L for a runner by selection_id
  const getPnl = (runner?: { selection_id?: number }) => {
    if (!positions || !runner?.selection_id) return undefined;
    const val = positions[String(runner.selection_id)];
    return val != null && val !== 0 ? val : undefined;
  };

  // Build clickable odds URL with encoded bet params (no plain-text exposure)
  const oddsUrl = (runner: { selection_id?: number; name?: string } | undefined, side: "back" | "lay", price: number) => {
    if (!runner || price <= 0) return marketHref;
    const payload = btoa(JSON.stringify({ s: runner.selection_id, d: side, p: price }));
    return `${marketHref}?b=${encodeURIComponent(payload)}`;
  };

  return (
    <div className={`border-b border-gray-800/15 last:border-0 hover:bg-white/[0.03] transition group ${
      isSuspended ? "opacity-50" : isLive ? "bg-green-500/[0.03]" : ""
    }`}>
      {/* ── DESKTOP: single row layout ── */}
      <div className="hidden sm:flex items-center">
        {/* Match info */}
        <Link href={marketHref} className="flex-1 min-w-0 px-2 py-1.5">
          <div className="flex items-center gap-1.5">
            {showLiveBadge && isLive && (
              <span className="w-2 h-2 bg-green-500 rounded-full animate-pulse flex-shrink-0" />
            )}
            <span className={`text-[12px] font-medium truncate ${isLive ? "text-white animate-pulse" : "text-gray-300 group-hover:text-white"}`}>
              {matchName}
            </span>
            {isSuspended && <span className="text-[8px] text-yellow-500 font-bold flex-shrink-0">SUSPENDED</span>}
          </div>
          <div className="flex items-center gap-2 mt-0.5 text-[9px] text-gray-500">
            <span>{market.sport?.toUpperCase()}</span>
            <span>
              {isLive ? "In-Play" : new Date(market.start_time).toLocaleString("en-IN", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })}
            </span>
            {(market.total_matched || 0) > 0 && <span className="font-mono tabular-nums">{fmtCurrency(market.total_matched || 0)}</span>}
          </div>
          {positions && (
            <div className="flex items-center gap-3 mt-0.5">
              {r1 && getPnl(r1) !== undefined && <PnlBadge label={r1.name} value={getPnl(r1)!} />}
              {rX && getPnl(rX) !== undefined && <PnlBadge label={rX.name} value={getPnl(rX)!} />}
              {r2 && getPnl(r2) !== undefined && <PnlBadge label={r2.name} value={getPnl(r2)!} />}
            </div>
          )}
        </Link>
        {/* Odds columns — desktop */}
        <div className="flex gap-px w-[360px] flex-shrink-0">
          <RunnerOddsClickable runner={r1} marketHref={marketHref} oddsUrl={oddsUrl} />
          <div className="flex-1 flex gap-px">
            {rX ? <RunnerOddsClickable runner={rX} marketHref={marketHref} oddsUrl={oddsUrl} /> : <EmptyOdds />}
          </div>
          <RunnerOddsClickable runner={r2} marketHref={marketHref} oddsUrl={oddsUrl} />
        </div>
      </div>

      {/* ── MOBILE: two row layout — name on top, full-width odds below ── */}
      <div className="sm:hidden">
        {/* Row 1: Match name */}
        <Link href={marketHref} className="block px-2 pt-1.5 pb-1">
          <div className="flex items-center gap-1.5">
            {showLiveBadge && isLive && (
              <span className="w-2 h-2 bg-green-500 rounded-full animate-pulse flex-shrink-0" />
            )}
            <span className={`text-[12px] font-medium ${isLive ? "text-white animate-pulse" : "text-gray-300"}`}>
              {matchName}
            </span>
            {isSuspended && <span className="text-[8px] text-yellow-500 font-bold ml-1">SUSPENDED</span>}
          </div>
          <div className="flex items-center gap-2 mt-0.5 text-[9px] text-gray-500">
            <span>{market.sport?.toUpperCase()}</span>
            <span>{isLive ? "In-Play" : new Date(market.start_time).toLocaleString("en-IN", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })}</span>
            {(market.total_matched || 0) > 0 && <span className="font-mono tabular-nums">{fmtCurrency(market.total_matched || 0)}</span>}
          </div>
          {positions && (
            <div className="flex items-center gap-3 mt-0.5">
              {r1 && getPnl(r1) !== undefined && <PnlBadge label={r1.name} value={getPnl(r1)!} />}
              {rX && getPnl(rX) !== undefined && <PnlBadge label={rX.name} value={getPnl(rX)!} />}
              {r2 && getPnl(r2) !== undefined && <PnlBadge label={r2.name} value={getPnl(r2)!} />}
            </div>
          )}
        </Link>

        {/* Row 2: Full-width odds — each cell is a clickable link */}
        <div className="flex gap-px px-1 pb-1.5">
          {/* Runner 1 label + odds */}
          <div className="flex-1 min-w-0">
            <div className="text-[8px] text-gray-500 text-center truncate px-0.5 mb-0.5">{r1?.name || "1"}</div>
            <div className="flex gap-px">
              <Link href={oddsUrl(r1, "back", r1?.back_prices?.[0]?.price || r1?.back_price || 0)}
                className="flex-1 bg-[#72BBEF] flex items-center justify-center py-2 min-h-[36px] active:brightness-90 transition">
                <span className="text-[12px] font-bold text-black tabular-nums">{(r1?.back_prices?.[0]?.price || r1?.back_price || 0) > 0 ? (r1?.back_prices?.[0]?.price || r1?.back_price || 0).toFixed(2) : "-"}</span>
              </Link>
              <Link href={oddsUrl(r1, "lay", r1?.lay_prices?.[0]?.price || r1?.lay_price || 0)}
                className="flex-1 bg-[#FAA9BA] flex items-center justify-center py-2 min-h-[36px] active:brightness-90 transition">
                <span className="text-[12px] font-bold text-black tabular-nums">{(r1?.lay_prices?.[0]?.price || r1?.lay_price || 0) > 0 ? (r1?.lay_prices?.[0]?.price || r1?.lay_price || 0).toFixed(2) : "-"}</span>
              </Link>
            </div>
          </div>
          {/* Draw (if exists) */}
          {rX && (
            <div className="flex-1 min-w-0">
              <div className="text-[8px] text-gray-500 text-center truncate px-0.5 mb-0.5">{rX.name || "X"}</div>
              <div className="flex gap-px">
                <Link href={oddsUrl(rX, "back", rX.back_prices?.[0]?.price || rX.back_price || 0)}
                  className="flex-1 bg-[#72BBEF] flex items-center justify-center py-2 min-h-[36px] active:brightness-90 transition">
                  <span className="text-[12px] font-bold text-black tabular-nums">{(rX.back_prices?.[0]?.price || rX.back_price || 0) > 0 ? (rX.back_prices?.[0]?.price || rX.back_price || 0).toFixed(2) : "-"}</span>
                </Link>
                <Link href={oddsUrl(rX, "lay", rX.lay_prices?.[0]?.price || rX.lay_price || 0)}
                  className="flex-1 bg-[#FAA9BA] flex items-center justify-center py-2 min-h-[36px] active:brightness-90 transition">
                  <span className="text-[12px] font-bold text-black tabular-nums">{(rX.lay_prices?.[0]?.price || rX.lay_price || 0) > 0 ? (rX.lay_prices?.[0]?.price || rX.lay_price || 0).toFixed(2) : "-"}</span>
                </Link>
              </div>
            </div>
          )}
          {/* Runner 2 */}
          <div className="flex-1 min-w-0">
            <div className="text-[8px] text-gray-500 text-center truncate px-0.5 mb-0.5">{r2?.name || "2"}</div>
            <div className="flex gap-px">
              <Link href={oddsUrl(r2, "back", r2?.back_prices?.[0]?.price || r2?.back_price || 0)}
                className="flex-1 bg-[#72BBEF] flex items-center justify-center py-2 min-h-[36px] active:brightness-90 transition">
                <span className="text-[12px] font-bold text-black tabular-nums">{(r2?.back_prices?.[0]?.price || r2?.back_price || 0) > 0 ? (r2?.back_prices?.[0]?.price || r2?.back_price || 0).toFixed(2) : "-"}</span>
              </Link>
              <Link href={oddsUrl(r2, "lay", r2?.lay_prices?.[0]?.price || r2?.lay_price || 0)}
                className="flex-1 bg-[#FAA9BA] flex items-center justify-center py-2 min-h-[36px] active:brightness-90 transition">
                <span className="text-[12px] font-bold text-black tabular-nums">{(r2?.lay_prices?.[0]?.price || r2?.lay_price || 0) > 0 ? (r2?.lay_prices?.[0]?.price || r2?.lay_price || 0).toFixed(2) : "-"}</span>
              </Link>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function PnlBadge({ label, value }: { label: string; value: number }) {
  const isPositive = value > 0;
  return (
    <span className={`text-[9px] font-semibold tabular-nums ${isPositive ? "text-green-400" : "text-red-400"}`}>
      {label}: {isPositive ? "+" : ""}{fmtCurrency(value)}
    </span>
  );
}

function RunnerOddsClickable({ runner, marketHref, oddsUrl }: {
  runner?: { selection_id?: number; name?: string; back_prices?: { price: number; size: number }[]; lay_prices?: { price: number; size: number }[]; back_price?: number; lay_price?: number };
  marketHref: string;
  oddsUrl: (r: typeof runner, side: "back" | "lay", price: number) => string;
}) {
  if (!runner) return <EmptyOdds />;
  const bp = runner.back_prices?.[0]?.price || runner.back_price || 0;
  const lp = runner.lay_prices?.[0]?.price || runner.lay_price || 0;
  return (
    <div className="flex-1 flex gap-px">
      <Link href={oddsUrl(runner, "back", bp)} className="flex-1 bg-[#72BBEF] hover:brightness-95 flex items-center justify-center py-1.5 min-h-[32px] transition">
        <span className="text-[11px] font-bold text-black tabular-nums">{bp > 0 ? bp.toFixed(2) : "-"}</span>
      </Link>
      <Link href={oddsUrl(runner, "lay", lp)} className="flex-1 bg-[#FAA9BA] hover:brightness-95 flex items-center justify-center py-1.5 min-h-[32px] transition">
        <span className="text-[11px] font-bold text-black tabular-nums">{lp > 0 ? lp.toFixed(2) : "-"}</span>
      </Link>
    </div>
  );
}

function RunnerOdds({ runner }: { runner?: { back_prices?: { price: number; size: number }[]; lay_prices?: { price: number; size: number }[]; back_price?: number; lay_price?: number } }) {
  if (!runner) return <EmptyOdds />;
  const bp = runner.back_prices?.[0]?.price || runner.back_price || 0;
  const lp = runner.lay_prices?.[0]?.price || runner.lay_price || 0;

  return (
    <div className="flex-1 flex gap-px">
      <div className="flex-1 bg-[#72BBEF] flex items-center justify-center py-1.5 min-h-[32px]">
        <span className="text-[11px] font-bold text-black tabular-nums">{bp > 0 ? bp.toFixed(2) : "-"}</span>
      </div>
      <div className="flex-1 bg-[#FAA9BA] flex items-center justify-center py-1.5 min-h-[32px]">
        <span className="text-[11px] font-bold text-black tabular-nums">{lp > 0 ? lp.toFixed(2) : "-"}</span>
      </div>
    </div>
  );
}

function EmptyOdds() {
  return (
    <div className="flex-1 flex gap-px">
      <div className="flex-1 bg-[#72BBEF]/30 flex items-center justify-center py-1.5 min-h-[32px]">
        <span className="text-[10px] text-black/20">-</span>
      </div>
      <div className="flex-1 bg-[#FAA9BA]/30 flex items-center justify-center py-1.5 min-h-[32px]">
        <span className="text-[10px] text-black/20">-</span>
      </div>
    </div>
  );
}

function fmtCurrency(n: number): string {
  const abs = Math.abs(n);
  const sign = n < 0 ? "-" : "";
  if (abs >= 10000000) return `${sign}₹${(abs / 10000000).toFixed(1)}Cr`;
  if (abs >= 100000) return `${sign}₹${(abs / 100000).toFixed(1)}L`;
  return `${sign}₹${abs.toLocaleString("en-IN", { maximumFractionDigits: 0 })}`;
}
