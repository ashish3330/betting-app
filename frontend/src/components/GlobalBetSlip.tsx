"use client";

/**
 * Persistent slide-out bet slip drawer. Mounted once in client-layout.tsx and
 * renders on top of every page that contains clickable odds. Pulls state from
 * the BetSlipProvider in `@/lib/betslip` and uses `api.placeBet` to submit.
 *
 * Features:
 *  - Slide-out panel (right side, desktop) / bottom sheet (mobile)
 *  - Multiple selections with per-selection stake input
 *  - Quick stake presets: 100 / 500 / 1K / 5K / 10K / MAX
 *  - Profit ("If wins") and Loss ("If loses") preview per selection + total
 *  - Floating toggle button with a selections count badge
 */

import { useCallback, useState } from "react";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useToast } from "@/components/Toast";
import { useBetSlip, BetSlipSelection } from "@/lib/betslip";

const QUICK_STAKES: number[] = [100, 500, 1000, 5000, 10000];

function formatStakeLabel(n: number): string {
  if (n >= 1000) return `${n / 1000}K`;
  return n.toString();
}

function formatCurrency(n: number): string {
  return `\u20B9${n.toLocaleString("en-IN", { maximumFractionDigits: 2 })}`;
}

interface Calc {
  win: number;
  loss: number;
}

function calcProfitLoss(sel: BetSlipSelection): Calc {
  const stake = sel.stake || 0;
  const price = sel.price || 0;
  if (stake <= 0 || price <= 0) return { win: 0, loss: 0 };
  if (sel.isSession) {
    // Session/fancy: YES (back) profits = stake * rate / 100, loses stake.
    // NO (lay) profits stake, loses stake * rate / 100.
    if (sel.side === "back") {
      return { win: (stake * price) / 100, loss: stake };
    }
    return { win: stake, loss: (stake * price) / 100 };
  }
  if (sel.side === "back") {
    return { win: stake * (price - 1), loss: stake };
  }
  // Lay: win = stake, lose = liability = stake * (price - 1)
  return { win: stake, loss: stake * (price - 1) };
}

export default function GlobalBetSlip() {
  const {
    selections,
    isOpen,
    open,
    close,
    removeSelection,
    updateSelection,
    acceptLivePrice,
    acceptAllLivePrices,
    clearAll,
  } = useBetSlip();
  const { isLoggedIn, balance, refreshBalance } = useAuth();
  const { addToast } = useToast();
  const [placing, setPlacing] = useState(false);

  const availableBalance = balance?.available_balance ?? 0;

  const placeAll = useCallback(async () => {
    if (!isLoggedIn) {
      addToast({
        type: "error",
        title: "Login required",
        message: "Please log in to place bets",
      });
      return;
    }
    const valid = selections.filter((s) => s.stake > 0 && s.price > 1);
    if (valid.length === 0) {
      addToast({ type: "warning", title: "Enter a stake for at least one selection" });
      return;
    }

    // Refuse to place any bet whose price has moved relative to live odds
    // unless the user has explicitly accepted (cleared the movement flag).
    // This prevents accidentally placing bets at stale prices.
    const moved = valid.filter((s) => s.movement);
    if (moved.length > 0) {
      addToast({
        type: "warning",
        title: "Prices have moved",
        message: `Accept the new odds for ${moved.length} selection${moved.length > 1 ? "s" : ""} before placing`,
      });
      return;
    }

    setPlacing(true);
    let successes = 0;
    let failures = 0;
    for (const sel of valid) {
      try {
        await api.placeBet({
          market_id: sel.marketId,
          selection_id: sel.selectionId,
          side: sel.side,
          price: sel.price,
          stake: sel.stake,
          client_ref: `slip_${Date.now()}_${Math.random()
            .toString(36)
            .slice(2, 8)}`,
        });
        successes += 1;
        removeSelection(sel.id);
      } catch (err) {
        failures += 1;
        const msg = err instanceof Error ? err.message : "Failed";
        addToast({
          type: "error",
          title: `Bet failed: ${sel.runnerName}`,
          message: msg,
        });
      }
    }
    setPlacing(false);
    refreshBalance();
    if (successes > 0) {
      addToast({
        type: "success",
        title: `${successes} bet${successes > 1 ? "s" : ""} placed`,
        message:
          failures > 0
            ? `${failures} failed — check errors above`
            : undefined,
      });
    }
  }, [
    isLoggedIn,
    selections,
    addToast,
    removeSelection,
    refreshBalance,
  ]);

  const totalStake = selections.reduce((sum, s) => sum + (s.stake || 0), 0);
  const totalWin = selections.reduce(
    (sum, s) => sum + calcProfitLoss(s).win,
    0,
  );
  const totalLoss = selections.reduce(
    (sum, s) => sum + calcProfitLoss(s).loss,
    0,
  );

  // Floating toggle — only shown when drawer is closed & there's at least one selection
  const floatingToggle =
    !isOpen && selections.length > 0 ? (
      <button
        onClick={open}
        className="fixed right-3 bottom-20 md:bottom-6 z-40 bg-lotus hover:bg-lotus-light text-white rounded-full shadow-lg px-4 py-3 flex items-center gap-2 transition"
        aria-label="Open bet slip"
      >
        <svg
          className="w-5 h-5"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2"
          />
        </svg>
        <span className="text-sm font-bold">
          Slip ({selections.length})
        </span>
      </button>
    ) : null;

  return (
    <>
      {floatingToggle}

      {/* Backdrop */}
      {isOpen && (
        <div
          className="fixed inset-0 bg-black/40 z-40 md:hidden"
          onClick={close}
        />
      )}

      {/* Drawer — sits above mobile bottom nav (bottom-14) on mobile */}
      <aside
        className={`fixed right-0 top-[50px] bottom-14 md:bottom-0 z-40
          w-full sm:w-[360px]
          bg-[var(--bg-surface)] border-l border-gray-800/60
          shadow-2xl
          flex flex-col
          transition-transform duration-300 ease-out
          ${isOpen ? "translate-x-0" : "translate-x-full"}
        `}
        aria-hidden={!isOpen}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-3 py-2 bg-[var(--bg-primary)] border-b border-gray-800/60 flex-shrink-0">
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-bold text-white">Bet Slip</h2>
            {selections.length > 0 && (
              <span className="text-[10px] font-bold bg-lotus text-white rounded-full w-5 h-5 flex items-center justify-center">
                {selections.length}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            {selections.some((s) => s.movement) && (
              <button
                onClick={acceptAllLivePrices}
                className="text-[10px] font-bold px-2 py-0.5 rounded bg-yellow-500 text-black hover:bg-yellow-400 transition"
                title="Accept all moved prices"
              >
                Accept all
              </button>
            )}
            {selections.length > 0 && (
              <button
                onClick={clearAll}
                className="text-[11px] text-gray-500 hover:text-red-400 transition"
              >
                Clear
              </button>
            )}
            <button
              onClick={close}
              className="text-gray-400 hover:text-white"
              aria-label="Close bet slip"
            >
              <svg
                className="w-5 h-5"
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
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto">
          {selections.length === 0 ? (
            <div className="px-4 py-10 text-center">
              <p className="text-[13px] text-gray-400">
                Your bet slip is empty
              </p>
              <p className="text-[11px] text-gray-600 mt-2">
                Click on any back/lay odds to add a selection
              </p>
            </div>
          ) : (
            <div className="divide-y divide-gray-800/40">
              {selections.map((sel) => (
                <SelectionRow
                  key={sel.id}
                  selection={sel}
                  availableBalance={availableBalance}
                  onRemove={() => removeSelection(sel.id)}
                  onUpdate={(patch) => updateSelection(sel.id, patch)}
                  onAcceptLive={acceptLivePrice}
                />
              ))}
            </div>
          )}
        </div>

        {/* Footer summary + place button */}
        {selections.length > 0 && (
          <div className="flex-shrink-0 border-t border-gray-800/60 bg-[var(--bg-primary)] px-3 py-3 space-y-2">
            <div className="flex justify-between text-[11px]">
              <span className="text-gray-400">Total Stake</span>
              <span className="text-white font-mono font-medium">
                {formatCurrency(totalStake)}
              </span>
            </div>
            <div className="flex justify-between text-[11px]">
              <span className="text-gray-400">If all win</span>
              <span className="text-profit font-mono font-bold">
                +{formatCurrency(totalWin)}
              </span>
            </div>
            <div className="flex justify-between text-[11px]">
              <span className="text-gray-400">If all lose</span>
              <span className="text-loss font-mono font-bold">
                -{formatCurrency(totalLoss)}
              </span>
            </div>
            <button
              onClick={placeAll}
              disabled={placing || totalStake <= 0}
              className="w-full mt-1 py-2 bg-lotus hover:bg-lotus-light disabled:bg-gray-700 disabled:text-gray-500 disabled:cursor-not-allowed text-white text-sm font-bold rounded transition"
            >
              {placing
                ? "Placing..."
                : `Place ${selections.length} Bet${
                    selections.length > 1 ? "s" : ""
                  }`}
            </button>
          </div>
        )}
      </aside>
    </>
  );
}

/* ---------- SelectionRow ---------- */

interface SelectionRowProps {
  selection: BetSlipSelection;
  availableBalance: number;
  onRemove: () => void;
  onUpdate: (patch: Partial<BetSlipSelection>) => void;
  onAcceptLive: (id: string) => void;
}

function SelectionRow({
  selection,
  availableBalance,
  onRemove,
  onUpdate,
  onAcceptLive,
}: SelectionRowProps) {
  const isBack = selection.side === "back";
  const { win, loss } = calcProfitLoss(selection);
  const sideClass = isBack
    ? "bg-back/10 border-back/30 text-back"
    : "bg-lay/10 border-lay/30 text-lay";
  // Determine if the live market price has moved away from the user's
  // committed price. We do not auto-overwrite — the user must accept.
  const priceMoved =
    selection.latestPrice != null &&
    selection.latestPrice > 0 &&
    selection.latestPrice !== selection.price;
  const moveDir = selection.movement;
  const label = selection.isSession
    ? isBack
      ? "YES"
      : "NO"
    : isBack
      ? "BACK"
      : "LAY";

  return (
    <div className="px-3 py-2.5 space-y-2">
      {/* Runner header */}
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="text-[12px] font-semibold text-white truncate">
            {selection.runnerName}
          </div>
          {(selection.eventName || selection.marketName) && (
            <div className="text-[10px] text-gray-500 truncate">
              {selection.eventName || selection.marketName}
            </div>
          )}
        </div>
        <button
          onClick={onRemove}
          className="text-gray-500 hover:text-red-400 flex-shrink-0"
          aria-label="Remove selection"
        >
          <svg
            className="w-4 h-4"
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

      {/* Side badge + odds + stake inputs */}
      <div className="grid grid-cols-2 gap-2">
        <div>
          <label className="text-[9px] text-gray-500 uppercase tracking-wide">
            Odds
          </label>
          <input
            type="number"
            step="0.01"
            min="1.01"
            value={selection.price}
            onChange={(e) =>
              onUpdate({
                price: parseFloat(e.target.value) || 1.01,
                // Clear the price-moved indicator when the user edits manually.
                latestPrice: undefined,
                movement: undefined,
              })
            }
            className={`mt-0.5 w-full h-8 px-2 text-sm text-center font-bold rounded border bg-[var(--bg-primary)] outline-none ${sideClass}`}
          />
        </div>
        <div>
          <label className="text-[9px] text-gray-500 uppercase tracking-wide">
            Stake ({"\u20B9"})
          </label>
          <input
            type="number"
            min="0"
            step="1"
            placeholder="0"
            value={selection.stake || ""}
            onChange={(e) =>
              onUpdate({ stake: parseFloat(e.target.value) || 0 })
            }
            className="mt-0.5 w-full h-8 px-2 text-sm font-mono bg-[var(--bg-primary)] border border-gray-700 rounded focus:outline-none focus:border-lotus text-white"
          />
        </div>
      </div>

      {/* Side label */}
      <div className="flex items-center gap-2">
        <span
          className={`text-[10px] font-bold px-2 py-0.5 rounded ${
            isBack ? "bg-back/20 text-back" : "bg-lay/20 text-lay"
          }`}
        >
          {label} @ {selection.price.toFixed(2)}
        </span>
      </div>

      {/* Price-moved banner — appears when the live market price differs
          from the user's committed price. Click to accept the new odds. */}
      {priceMoved && (
        <div
          className={`flex items-center justify-between gap-2 px-2 py-1.5 rounded border text-[11px] ${
            moveDir === "up"
              ? "bg-profit/10 border-profit/30"
              : moveDir === "down"
                ? "bg-loss/10 border-loss/30"
                : "bg-yellow-500/10 border-yellow-500/30"
          }`}
        >
          <div className="flex items-center gap-1.5 min-w-0">
            <span
              className={`text-base leading-none ${
                moveDir === "up"
                  ? "text-profit"
                  : moveDir === "down"
                    ? "text-loss"
                    : "text-yellow-400"
              }`}
            >
              {moveDir === "up" ? "▲" : moveDir === "down" ? "▼" : "↻"}
            </span>
            <span className="text-gray-300 truncate">
              Price moved to{" "}
              <span className="font-mono font-bold text-white">
                {selection.latestPrice?.toFixed(2)}
              </span>
            </span>
          </div>
          <button
            onClick={() => onAcceptLive(selection.id)}
            className={`flex-shrink-0 text-[10px] font-bold px-2 py-0.5 rounded transition ${
              moveDir === "up"
                ? "bg-profit text-black hover:bg-profit/80"
                : moveDir === "down"
                  ? "bg-loss text-white hover:bg-loss/80"
                  : "bg-yellow-500 text-black hover:bg-yellow-500/80"
            }`}
          >
            Accept
          </button>
        </div>
      )}

      {/* Quick stake buttons */}
      <div className="grid grid-cols-6 gap-1">
        {QUICK_STAKES.map((amount) => (
          <button
            key={amount}
            onClick={() => onUpdate({ stake: amount })}
            className={`text-[10px] py-1 rounded font-medium transition ${
              selection.stake === amount
                ? isBack
                  ? "bg-back text-black"
                  : "bg-lay text-black"
                : "bg-gray-800/60 text-gray-300 hover:bg-gray-700 hover:text-white"
            }`}
          >
            {formatStakeLabel(amount)}
          </button>
        ))}
        <button
          onClick={() =>
            onUpdate({ stake: Math.max(0, Math.floor(availableBalance)) })
          }
          disabled={availableBalance <= 0}
          className="text-[10px] py-1 rounded font-bold bg-lotus/20 text-lotus hover:bg-lotus/30 disabled:opacity-40 disabled:cursor-not-allowed transition"
          title={`Max: ${formatCurrency(availableBalance)}`}
        >
          MAX
        </button>
      </div>

      {/* Profit / Loss preview */}
      {selection.stake > 0 && selection.price > 1 && (
        <div className="grid grid-cols-2 gap-2 pt-1">
          <div className="text-[10px] bg-profit/10 border border-profit/20 rounded px-2 py-1">
            <div className="text-gray-500">If wins</div>
            <div className="text-profit font-mono font-bold">
              +{formatCurrency(win)}
            </div>
          </div>
          <div className="text-[10px] bg-loss/10 border border-loss/20 rounded px-2 py-1">
            <div className="text-gray-500">If loses</div>
            <div className="text-loss font-mono font-bold">
              -{formatCurrency(loss)}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
