"use client";

import { useEffect, useRef, useState } from "react";
import { OrderBookLevel } from "@/lib/api";

interface RunnerOrderBook {
  runnerId: string;
  runnerName: string;
  back: OrderBookLevel[];
  lay: OrderBookLevel[];
  status?: string;
}

interface OrderBookProps {
  runners: RunnerOrderBook[];
  onCellClick?: (
    runnerId: string,
    runnerName: string,
    side: "back" | "lay",
    price: number
  ) => void;
}

function padLevels(levels: OrderBookLevel[], count: number): OrderBookLevel[] {
  const padded = [...levels];
  while (padded.length < count) {
    padded.push({ price: 0, size: 0 });
  }
  return padded.slice(0, count);
}

function formatAmount(n: number): string {
  if (!n) return "";
  if (n >= 100000) return `${(n / 100000).toFixed(1)}L`;
  if (n >= 1000) return `${(n / 1000).toFixed(1)}K`;
  return n.toFixed(0);
}

export default function OrderBook({ runners, onCellClick }: OrderBookProps) {
  const [prevPrices, setPrevPrices] = useState<
    Map<string, { back: number; lay: number }>
  >(new Map());
  const [flashState, setFlashState] = useState<
    Map<string, { back: "up" | "down" | null; lay: "up" | "down" | null }>
  >(new Map());

  // Track price changes for flash animation
  useEffect(() => {
    const newFlash = new Map<
      string,
      { back: "up" | "down" | null; lay: "up" | "down" | null }
    >();

    runners.forEach((r) => {
      const prev = prevPrices.get(r.runnerId);
      const currentBack = r.back[0]?.price || 0;
      const currentLay = r.lay[0]?.price || 0;

      let backFlash: "up" | "down" | null = null;
      let layFlash: "up" | "down" | null = null;

      if (prev) {
        if (prev.back > 0 && currentBack !== prev.back) {
          backFlash = currentBack > prev.back ? "up" : "down";
        }
        if (prev.lay > 0 && currentLay !== prev.lay) {
          layFlash = currentLay > prev.lay ? "up" : "down";
        }
      }

      newFlash.set(r.runnerId, { back: backFlash, lay: layFlash });
    });

    setFlashState(newFlash);

    // Update prev prices
    const newPrev = new Map<string, { back: number; lay: number }>();
    runners.forEach((r) => {
      newPrev.set(r.runnerId, {
        back: r.back[0]?.price || 0,
        lay: r.lay[0]?.price || 0,
      });
    });
    setPrevPrices(newPrev);

    // Clear flashes
    const timeout = setTimeout(() => {
      setFlashState(new Map());
    }, 600);

    return () => clearTimeout(timeout);
  }, [runners]);

  // Compute max sizes for volume bars
  const allBackSizes = runners.flatMap((r) => r.back.map((l) => l.size || 0));
  const allLaySizes = runners.flatMap((r) => r.lay.map((l) => l.size || 0));
  const maxBackSize = Math.max(...allBackSizes, 1);
  const maxLaySize = Math.max(...allLaySizes, 1);

  return (
    <div className="bg-[var(--bg-surface)] rounded-lg border border-gray-800/60 overflow-hidden">
      {/* Header */}
      <div className="px-3 py-2 border-b border-gray-800/60">
        <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
          Order Book
        </h3>
      </div>

      {/* Column headers: Back 3 | Back 2 | Back 1 | Runner | Lay 1 | Lay 2 | Lay 3 */}
      <div className="hidden md:grid grid-cols-[repeat(3,72px)_1fr_repeat(3,72px)] border-b border-gray-800/40">
        <div className="text-center py-1.5 text-[10px] text-back/40 font-medium">
          Back
        </div>
        <div className="text-center py-1.5 text-[10px] text-back/60 font-medium">
          Back
        </div>
        <div className="text-center py-1.5 text-[10px] text-back font-medium">
          Back
        </div>
        <div className="text-center py-1.5 text-[10px] text-gray-500 font-medium" />
        <div className="text-center py-1.5 text-[10px] text-lay font-medium">
          Lay
        </div>
        <div className="text-center py-1.5 text-[10px] text-lay/60 font-medium">
          Lay
        </div>
        <div className="text-center py-1.5 text-[10px] text-lay/40 font-medium">
          Lay
        </div>
      </div>

      {/* Mobile column headers */}
      <div className="grid md:hidden grid-cols-[64px_1fr_64px] border-b border-gray-800/40">
        <div className="text-center py-1.5 text-[10px] text-back font-medium">
          Back
        </div>
        <div />
        <div className="text-center py-1.5 text-[10px] text-lay font-medium">
          Lay
        </div>
      </div>

      {/* Runners */}
      {runners.map((runner) => {
        const backLevels = padLevels(runner.back, 3);
        const layLevels = padLevels(runner.lay, 3);
        const isSuspended = runner.status === "suspended";
        const flash = flashState.get(runner.runnerId);

        return (
          <div
            key={runner.runnerId}
            className={`border-b border-gray-800/30 ${
              isSuspended ? "suspended" : ""
            }`}
          >
            {/* Desktop layout */}
            <div
              className="hidden md:grid grid-cols-[repeat(3,72px)_1fr_repeat(3,72px)] hover:bg-white/[0.02] transition-colors"
              style={{ minHeight: "36px" }}
            >
              {/* Back cells - reversed so best is closest to runner name */}
              {[...backLevels].reverse().map((level, i) => {
                const isBest = i === 2;
                const volumeWidth = level.size
                  ? `${(level.size / maxBackSize) * 100}%`
                  : "0%";

                return (
                  <button
                    key={`back-${i}`}
                    onClick={() =>
                      level.price > 0 &&
                      onCellClick?.(
                        runner.runnerId,
                        runner.runnerName,
                        "back",
                        level.price
                      )
                    }
                    disabled={level.price <= 0 || isSuspended}
                    className={`relative flex flex-col items-center justify-center py-1 transition-colors cursor-pointer disabled:cursor-default ${
                      isBest
                        ? "bg-[#72bbef]/30 hover:bg-[#72bbef]/40"
                        : i === 1
                        ? "bg-[#72bbef]/20 hover:bg-[#72bbef]/30"
                        : "bg-[#72bbef]/10 hover:bg-[#72bbef]/20"
                    } ${
                      isBest && flash?.back === "up"
                        ? "odds-up"
                        : isBest && flash?.back === "down"
                        ? "odds-down"
                        : ""
                    }`}
                  >
                    {/* Volume bar */}
                    <div
                      className="absolute bottom-0 left-0 h-[3px] bg-[#72bbef]/40 transition-all duration-300"
                      style={{ width: volumeWidth }}
                    />
                    <span className="text-xs font-bold text-[#1a1a2e]">
                      {level.price > 0 ? level.price.toFixed(2) : "-"}
                    </span>
                    <span className="text-[9px] text-[#1a1a2e]/60">
                      {formatAmount(level.size)}
                    </span>
                  </button>
                );
              })}

              {/* Runner name */}
              <div className="flex items-center px-3">
                <span className="text-sm text-white font-medium truncate">
                  {runner.runnerName}
                </span>
              </div>

              {/* Lay cells */}
              {layLevels.map((level, i) => {
                const isBest = i === 0;
                const volumeWidth = level.size
                  ? `${(level.size / maxLaySize) * 100}%`
                  : "0%";

                return (
                  <button
                    key={`lay-${i}`}
                    onClick={() =>
                      level.price > 0 &&
                      onCellClick?.(
                        runner.runnerId,
                        runner.runnerName,
                        "lay",
                        level.price
                      )
                    }
                    disabled={level.price <= 0 || isSuspended}
                    className={`relative flex flex-col items-center justify-center py-1 transition-colors cursor-pointer disabled:cursor-default ${
                      isBest
                        ? "bg-[#faa9ba]/30 hover:bg-[#faa9ba]/40"
                        : i === 1
                        ? "bg-[#faa9ba]/20 hover:bg-[#faa9ba]/30"
                        : "bg-[#faa9ba]/10 hover:bg-[#faa9ba]/20"
                    } ${
                      isBest && flash?.lay === "up"
                        ? "odds-up"
                        : isBest && flash?.lay === "down"
                        ? "odds-down"
                        : ""
                    }`}
                  >
                    <div
                      className="absolute bottom-0 right-0 h-[3px] bg-[#faa9ba]/40 transition-all duration-300"
                      style={{ width: volumeWidth }}
                    />
                    <span className="text-xs font-bold text-[#1a1a2e]">
                      {level.price > 0 ? level.price.toFixed(2) : "-"}
                    </span>
                    <span className="text-[9px] text-[#1a1a2e]/60">
                      {formatAmount(level.size)}
                    </span>
                  </button>
                );
              })}
            </div>

            {/* Mobile layout: 1 back | runner | 1 lay */}
            <div
              className="grid md:hidden grid-cols-[64px_1fr_64px] hover:bg-white/[0.02] transition-colors"
              style={{ minHeight: "36px" }}
            >
              {/* Best back */}
              <button
                onClick={() =>
                  backLevels[0]?.price > 0 &&
                  onCellClick?.(
                    runner.runnerId,
                    runner.runnerName,
                    "back",
                    backLevels[0].price
                  )
                }
                disabled={!backLevels[0]?.price || isSuspended}
                className={`flex flex-col items-center justify-center py-1 bg-[#72bbef]/30 hover:bg-[#72bbef]/40 transition-colors ${
                  flash?.back === "up"
                    ? "odds-up"
                    : flash?.back === "down"
                    ? "odds-down"
                    : ""
                }`}
              >
                <span className="text-xs font-bold text-[#1a1a2e]">
                  {backLevels[0]?.price > 0
                    ? backLevels[0].price.toFixed(2)
                    : "-"}
                </span>
                <span className="text-[9px] text-[#1a1a2e]/60">
                  {formatAmount(backLevels[0]?.size || 0)}
                </span>
              </button>

              {/* Runner name */}
              <div className="flex items-center px-2">
                <span className="text-xs text-white font-medium truncate">
                  {runner.runnerName}
                </span>
              </div>

              {/* Best lay */}
              <button
                onClick={() =>
                  layLevels[0]?.price > 0 &&
                  onCellClick?.(
                    runner.runnerId,
                    runner.runnerName,
                    "lay",
                    layLevels[0].price
                  )
                }
                disabled={!layLevels[0]?.price || isSuspended}
                className={`flex flex-col items-center justify-center py-1 bg-[#faa9ba]/30 hover:bg-[#faa9ba]/40 transition-colors ${
                  flash?.lay === "up"
                    ? "odds-up"
                    : flash?.lay === "down"
                    ? "odds-down"
                    : ""
                }`}
              >
                <span className="text-xs font-bold text-[#1a1a2e]">
                  {layLevels[0]?.price > 0
                    ? layLevels[0].price.toFixed(2)
                    : "-"}
                </span>
                <span className="text-[9px] text-[#1a1a2e]/60">
                  {formatAmount(layLevels[0]?.size || 0)}
                </span>
              </button>
            </div>
          </div>
        );
      })}

      {runners.length === 0 && (
        <div className="px-4 py-6 text-center text-xs text-gray-400">
          No order book data available
        </div>
      )}
    </div>
  );
}
