"use client";

import { useState, useCallback, useRef, useEffect } from "react";
import { api, Runner } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useToast } from "@/components/Toast";

export interface BetSelection {
  id: string;
  marketId: string;
  runner: Runner;
  side: "back" | "lay";
  price: number;
  stake: number;
  isSession?: boolean; // Session/fancy bets use rate (profit = stake * rate / 100), not odds
}

interface BetSlipProps {
  selections: BetSelection[];
  onRemoveSelection: (id: string) => void;
  onClearAll: () => void;
  onUpdateSelection: (id: string, updates: Partial<BetSelection>) => void;
  onBetPlaced?: () => void;
}

const QUICK_STAKES = [100, 500, 1000, 5000, 10000, 25000];

function formatStakeLabel(n: number): string {
  if (n >= 1000) return `${n / 1000}K`;
  return n.toString();
}

export default function BetSlip({
  selections,
  onRemoveSelection,
  onClearAll,
  onUpdateSelection,
  onBetPlaced,
}: BetSlipProps) {
  const { isLoggedIn, refreshBalance } = useAuth();
  const { addToast } = useToast();
  const [oneClickMode, setOneClickMode] = useState(false);
  const [confirmBetId, setConfirmBetId] = useState<string | null>(null);
  const [oneClickStake, setOneClickStake] = useState(100);
  const [placingBets, setPlacingBets] = useState<Set<string>>(new Set());
  const [results, setResults] = useState<
    Map<string, { success: boolean; message: string }>
  >(new Map());

  const backSelections = selections.filter((s) => s.side === "back");
  const laySelections = selections.filter((s) => s.side === "lay");

  const placeSingleBet = useCallback(
    async (selection: BetSelection) => {
      if (!isLoggedIn) {
        setResults((prev) => {
          const next = new Map(prev);
          next.set(selection.id, {
            success: false,
            message: "Please login to place bets",
          });
          return next;
        });
        return;
      }

      if (selection.stake <= 0 || selection.price <= 1) return;

      setPlacingBets((prev) => new Set(prev).add(selection.id));
      setResults((prev) => {
        const next = new Map(prev);
        next.delete(selection.id);
        return next;
      });

      try {
        const result = await api.placeBet({
          market_id: selection.marketId,
          selection_id: selection.runner.selection_id || parseInt(String(selection.runner.id), 10) || 0,
          side: selection.side,
          price: selection.price,
          stake: selection.stake,
          client_ref: `web_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
        });

        setResults((prev) => {
          const next = new Map(prev);
          next.set(selection.id, {
            success: true,
            message: `Placed! ID: ${result.bet_id}${
              result.matched_stake
                ? ` | Matched: \u20B9${result.matched_stake}`
                : ""
            }`,
          });
          return next;
        });
        addToast({
          type: "success",
          title: "Bet Placed",
          message: `${selection.side.toUpperCase()} ${selection.runner.name} @ ${selection.price} for \u20B9${selection.stake}`,
        });
        refreshBalance();
        onBetPlaced?.();

        // Auto-remove successful bet after 3s
        setTimeout(() => {
          onRemoveSelection(selection.id);
          setResults((prev) => {
            const next = new Map(prev);
            next.delete(selection.id);
            return next;
          });
        }, 3000);
      } catch (err) {
        let errMsg = err instanceof Error ? err.message : "Failed to place bet";
        let toastTitle = "Bet Failed";

        // Handle ODDS_CHANGED — update price in selection automatically
        if (errMsg.includes("Odds moved") || errMsg.includes("odds have changed")) {
          toastTitle = "Odds Changed";
          // Try to extract new price from error message
          const match = errMsg.match(/to (\d+\.\d+)/);
          if (match) {
            const newPrice = parseFloat(match[1]);
            onUpdateSelection(selection.id, { price: newPrice });
            errMsg = `Price updated to ${newPrice}. Please confirm and place again.`;
          }
        }

        setResults((prev) => {
          const next = new Map(prev);
          next.set(selection.id, { success: false, message: errMsg });
          return next;
        });
        addToast({ type: "warning", title: toastTitle, message: errMsg });
      } finally {
        setPlacingBets((prev) => {
          const next = new Set(prev);
          next.delete(selection.id);
          return next;
        });
      }
    },
    [isLoggedIn, refreshBalance, onRemoveSelection]
  );

  const placeAllBets = useCallback(async () => {
    const validSelections = selections.filter(
      (s) => s.stake > 0 && s.price > 1
    );
    for (const sel of validSelections) {
      await placeSingleBet(sel);
    }
  }, [selections, placeSingleBet]);

  const totalStake = selections.reduce((sum, s) => sum + (s.stake || 0), 0);
  const totalProfit = selections.reduce((sum, s) => {
    if (s.isSession) {
      // Session: profit = stake * rate / 100 (rate IS the price field, e.g. 85)
      return sum + (s.side === "back" ? s.stake * s.price / 100 : s.stake);
    }
    if (s.side === "back") return sum + s.stake * (s.price - 1);
    return sum + s.stake;
  }, 0);
  const totalLiability = selections.reduce((sum, s) => {
    if (s.isSession) {
      return sum + (s.side === "lay" ? s.stake * s.price / 100 : s.stake);
    }
    if (s.side === "lay") return sum + s.stake * (s.price - 1);
    return sum + s.stake;
  }, 0);

  const adjustPrice = (selectionId: string, delta: number) => {
    const sel = selections.find((s) => s.id === selectionId);
    if (!sel) return;
    const newPrice = Math.max(1.01, sel.price + delta);
    onUpdateSelection(selectionId, { price: parseFloat(newPrice.toFixed(2)) });
  };

  return (
    <div className="sticky top-[50px] w-full bg-[var(--bg-surface)] border border-gray-800/60 rounded-lg overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2 bg-[var(--bg-primary)] border-b border-gray-800/60">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-semibold text-white">Bet Slip</h3>
          {selections.length > 0 && (
            <span className="text-[10px] font-bold bg-lotus text-white rounded-full w-5 h-5 flex items-center justify-center">
              {selections.length}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          {/* One-click toggle */}
          <button
            onClick={() => setOneClickMode(!oneClickMode)}
            className={`text-[10px] font-semibold px-2 py-1 rounded transition ${
              oneClickMode
                ? "bg-lotus text-white"
                : "bg-gray-800 text-gray-400 hover:text-white"
            }`}
          >
            1-Click
          </button>
          {selections.length > 0 && (
            <button
              onClick={onClearAll}
              className="text-[10px] text-gray-500 hover:text-red-400 transition"
            >
              Clear All
            </button>
          )}
        </div>
      </div>

      {/* One-click stake selector */}
      {oneClickMode && (
        <div className="px-2 py-1.5 bg-lotus/5 border-b border-lotus/20">
          <div className="text-[9px] text-lotus font-medium mb-1">
            One-Click Stake
          </div>
          <div className="flex gap-0.5">
            {QUICK_STAKES.map((amount) => (
              <button
                key={amount}
                onClick={() => setOneClickStake(amount)}
                className={`flex-1 text-[8px] py-0.5 rounded font-medium transition ${
                  oneClickStake === amount
                    ? "bg-lotus text-white"
                    : "bg-gray-800 text-gray-400 hover:bg-gray-700"
                }`}
              >
                {formatStakeLabel(amount)}
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Empty state */}
      {selections.length === 0 && (
        <div className="px-3 py-5 text-center">
          <p className="text-[11px] text-gray-500">
            Click on any odds to add a selection
          </p>
        </div>
      )}

      {/* Back selections */}
      {backSelections.length > 0 && (
        <div>
          <div className="px-2 py-1 bg-back/5 border-b border-back/20">
            <span className="text-[9px] font-semibold text-back uppercase tracking-wider">
              Back
            </span>
          </div>
          {backSelections.map((sel) => (
            <SelectionRow
              key={sel.id}
              selection={sel}
              isPlacing={placingBets.has(sel.id)}
              result={results.get(sel.id)}
              onRemove={() => onRemoveSelection(sel.id)}
              onUpdateStake={(stake) => onUpdateSelection(sel.id, { stake })}
              onUpdatePrice={(price) => onUpdateSelection(sel.id, { price })}
              onAdjustPrice={(delta) => adjustPrice(sel.id, delta)}
              onPlace={() => placeSingleBet(sel)}
              quickStakes={QUICK_STAKES}
            />
          ))}
        </div>
      )}

      {/* Lay selections */}
      {laySelections.length > 0 && (
        <div>
          <div className="px-2 py-1 bg-lay/5 border-b border-lay/20">
            <span className="text-[9px] font-semibold text-lay uppercase tracking-wider">
              Lay
            </span>
          </div>
          {laySelections.map((sel) => (
            <SelectionRow
              key={sel.id}
              selection={sel}
              isPlacing={placingBets.has(sel.id)}
              result={results.get(sel.id)}
              onRemove={() => onRemoveSelection(sel.id)}
              onUpdateStake={(stake) => onUpdateSelection(sel.id, { stake })}
              onUpdatePrice={(price) => onUpdateSelection(sel.id, { price })}
              onAdjustPrice={(delta) => adjustPrice(sel.id, delta)}
              onPlace={() => placeSingleBet(sel)}
              quickStakes={QUICK_STAKES}
            />
          ))}
        </div>
      )}

      {/* Summary and Place All */}
      {selections.length > 0 && (
        <div className="border-t border-gray-800/60">
          {/* Profit / Liability summary */}
          <div className="px-2 py-1.5 space-y-0.5 bg-surface/50">
            <div className="flex justify-between text-[10px]">
              <span className="text-gray-500">Total Stake</span>
              <span className="text-white font-mono font-medium">
                {"\u20B9"}
                {totalStake.toLocaleString("en-IN")}
              </span>
            </div>
            <div className="flex justify-between text-[10px]">
              <span className="text-gray-500">Est. Profit</span>
              <span className="text-profit font-mono font-medium">
                +{"\u20B9"}
                {totalProfit.toLocaleString("en-IN", {
                  maximumFractionDigits: 2,
                })}
              </span>
            </div>
            <div className="flex justify-between text-[10px]">
              <span className="text-gray-500">Max Liability</span>
              <span className="text-loss font-mono font-medium">
                -{"\u20B9"}
                {totalLiability.toLocaleString("en-IN", {
                  maximumFractionDigits: 2,
                })}
              </span>
            </div>
          </div>

          {/* Place all button */}
          <div className="px-2 py-1.5">
            <button
              onClick={placeAllBets}
              disabled={
                placingBets.size > 0 ||
                selections.every((s) => s.stake <= 0 || s.price <= 1)
              }
              className="w-full py-1.5 bg-lotus hover:bg-lotus-light disabled:bg-gray-700 disabled:text-gray-500 text-white text-xs font-bold rounded transition disabled:cursor-not-allowed"
            >
              {placingBets.size > 0 ? (
                <span className="flex items-center justify-center gap-1.5">
                  <span className="w-3 h-3 border-2 border-[#0f0f23]/30 border-t-[#0f0f23] rounded-full animate-spin" />
                  Placing...
                </span>
              ) : (
                `Place ${selections.length} Bet${selections.length > 1 ? "s" : ""}`
              )}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

/* ---------- Selection Row ---------- */

interface SelectionRowProps {
  selection: BetSelection;
  isPlacing: boolean;
  result?: { success: boolean; message: string };
  onRemove: () => void;
  onUpdateStake: (stake: number) => void;
  onUpdatePrice: (price: number) => void;
  onAdjustPrice: (delta: number) => void;
  onPlace: () => void;
  quickStakes: number[];
}

function SelectionRow({
  selection,
  isPlacing,
  result,
  onRemove,
  onUpdateStake,
  onUpdatePrice,
  onAdjustPrice,
  onPlace,
  quickStakes,
}: SelectionRowProps) {
  const isBack = selection.side === "back";
  const profit = isBack
    ? selection.stake * (selection.price - 1)
    : selection.stake;
  const liability = isBack
    ? selection.stake
    : selection.stake * (selection.price - 1);

  // Flash when price changes from live sync
  const prevPriceRef = useRef(selection.price);
  const [priceFlash, setPriceFlash] = useState<"up" | "down" | null>(null);

  useEffect(() => {
    if (selection.price !== prevPriceRef.current) {
      setPriceFlash(selection.price > prevPriceRef.current ? "up" : "down");
      prevPriceRef.current = selection.price;
      const timer = setTimeout(() => setPriceFlash(null), 1500);
      return () => clearTimeout(timer);
    }
  }, [selection.price]);

  return (
    <div
      className={`border-b border-gray-800/30 animate-in px-2 py-1.5 space-y-1 ${
        result?.success ? "bg-profit/5" : ""
      } ${priceFlash === "up" ? "bg-profit/10" : priceFlash === "down" ? "bg-loss/10" : ""}`}
      style={{ transition: "background-color 0.3s ease" }}
    >
      {/* Top row: runner name + close */}
      <div className="flex items-center justify-between">
        <div className="min-w-0 flex items-center gap-1.5">
          <span className="text-[11px] font-bold text-white truncate">
            {selection.runner.name}
          </span>
          <span
            className={`text-[9px] font-semibold ${
              isBack ? "text-back" : "text-lay"
            }`}
          >
            {isBack ? "BACK" : "LAY"} @ {selection.price.toFixed(2)}
            {priceFlash === "up" && " \u25B2"}
            {priceFlash === "down" && " \u25BC"}
          </span>
        </div>
        <button
          onClick={onRemove}
          className="p-0 text-gray-500 hover:text-red-400 transition flex-shrink-0"
        >
          <svg
            className="w-3.5 h-3.5"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M6 18L18 6M6 6l12 12"
            />
          </svg>
        </button>
      </div>

      {/* Price + Stake inputs */}
      <div className="grid grid-cols-2 gap-1.5">
        {/* Price */}
        <div>
          <label className="text-[9px] text-gray-500 uppercase tracking-wide">
            Odds
          </label>
          <div className="flex items-center gap-0.5 mt-0.5">
            <button
              onClick={() => onAdjustPrice(-0.01)}
              className="w-5 h-7 flex items-center justify-center bg-gray-800 rounded text-gray-400 hover:text-white text-[10px] transition"
            >
              -
            </button>
            <input
              type="number"
              step="0.01"
              min="1.01"
              value={selection.price}
              onChange={(e) =>
                onUpdatePrice(parseFloat(e.target.value) || 1.01)
              }
              className={`flex-1 h-7 text-center text-[11px] font-bold rounded border bg-[var(--bg-primary)] focus:outline-none ${
                isBack
                  ? "border-back/30 text-back focus:border-back"
                  : "border-lay/30 text-lay focus:border-lay"
              }`}
            />
            <button
              onClick={() => onAdjustPrice(0.01)}
              className="w-5 h-7 flex items-center justify-center bg-gray-800 rounded text-gray-400 hover:text-white text-[10px] transition"
            >
              +
            </button>
          </div>
        </div>

        {/* Stake */}
        <div>
          <label className="text-[9px] text-gray-500 uppercase tracking-wide">
            Stake ({"\u20B9"})
          </label>
          <input
            type="number"
            min="100"
            step="1"
            placeholder="0"
            value={selection.stake || ""}
            onChange={(e) =>
              onUpdateStake(parseFloat(e.target.value) || 0)
            }
            className="mt-0.5 w-full h-7 px-2 text-xs font-mono bg-[var(--bg-primary)] border border-gray-700 rounded focus:outline-none focus:border-lotus text-[var(--text-primary)]"
          />
        </div>
      </div>

      {/* Quick stake buttons */}
      <div className="flex gap-0.5">
        {quickStakes.map((amount) => (
          <button
            key={amount}
            onClick={() => onUpdateStake(amount)}
            className={`flex-1 text-[8px] py-0.5 rounded font-medium transition ${
              selection.stake === amount
                ? isBack
                  ? "bg-back text-[#0f0f23]"
                  : "bg-lay text-[#0f0f23]"
                : "bg-gray-800/60 text-gray-400 hover:bg-gray-700 hover:text-white"
            }`}
          >
            {formatStakeLabel(amount)}
          </button>
        ))}
      </div>

      {/* Profit / Liability */}
      {selection.stake > 0 && selection.price > 1 && (
        <div className="flex justify-between text-[10px]">
          <span className="text-gray-500">
            {isBack ? "Profit" : "Liability"}
          </span>
          <span
            className={`font-mono font-bold ${
              isBack ? "text-profit" : "text-loss"
            }`}
          >
            {isBack ? "+" : "-"}
            {"\u20B9"}
            {(isBack ? profit : liability).toLocaleString("en-IN", {
              maximumFractionDigits: 2,
            })}
          </span>
        </div>
      )}

      {/* Result message */}
      {result && (
        <div
          className={`text-[10px] px-2 py-1 rounded ${
            result.success
              ? "bg-profit/10 text-profit border border-profit/20"
              : "bg-loss/10 text-loss border border-loss/20"
          }`}
        >
          {result.message}
        </div>
      )}
    </div>
  );
}
