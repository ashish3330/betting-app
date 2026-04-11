"use client";

/**
 * Global bet slip store.
 *
 * Provides a React Context based store for bet selections that persist across
 * pages. Any component that shows odds can call `useBetSlip().addSelection(...)`
 * to push a selection. The mounted <GlobalBetSlip/> drawer reads the same store
 * and renders a persistent slide-out slip on the right.
 */

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  useEffect,
  ReactNode,
  createElement,
} from "react";

export interface BetSlipSelection {
  /** Stable key — typically `${marketId}:${selectionId}:${side}`. */
  id: string;
  marketId: string;
  marketName?: string;
  eventName?: string;
  selectionId: number;
  runnerName: string;
  side: "back" | "lay";
  /** The committed price the user is willing to bet at. */
  price: number;
  stake: number;
  /** True for session/fancy bets (profit = stake * rate / 100 instead of odds). */
  isSession?: boolean;
  /**
   * Live market price as last seen on the markets feed. Updated by the
   * markets page WS handler via syncLivePrice(). When this differs from
   * `price`, the slip UI shows a "price moved" indicator.
   */
  latestPrice?: number;
  /**
   * Direction of the last price movement (relative to `price`).
   * - "up": latestPrice > price
   * - "down": latestPrice < price
   * - undefined: no movement
   */
  movement?: "up" | "down";
}

/** Live runner price snapshot used by syncLivePrice. */
export interface LiveRunnerPrice {
  selectionId: number;
  backPrice?: number;
  layPrice?: number;
}

interface BetSlipContextValue {
  selections: BetSlipSelection[];
  isOpen: boolean;
  open: () => void;
  close: () => void;
  toggle: () => void;
  addSelection: (
    sel: Omit<BetSlipSelection, "id" | "stake"> & { stake?: number },
  ) => void;
  removeSelection: (id: string) => void;
  updateSelection: (id: string, patch: Partial<BetSlipSelection>) => void;
  clearAll: () => void;
  /**
   * Update `latestPrice` for any selections in the slip that match the given
   * marketId. Called by the markets page WS handler whenever odds change.
   * Does NOT mutate `price` — that stays as the user's committed value
   * until they explicitly accept the new odds.
   */
  syncLivePrices: (marketId: string, runners: LiveRunnerPrice[]) => void;
  /**
   * Accept the latest market price for a selection. Sets `price = latestPrice`
   * and clears the movement indicator.
   */
  acceptLivePrice: (id: string) => void;
  /** Accept latest market price for every selection that has moved. */
  acceptAllLivePrices: () => void;
}

const BetSlipContext = createContext<BetSlipContextValue | null>(null);

export function BetSlipProvider({ children }: { children: ReactNode }) {
  const [selections, setSelections] = useState<BetSlipSelection[]>([]);
  const [isOpen, setIsOpen] = useState(false);

  // Auto-open drawer when a selection is added
  const addSelection = useCallback(
    (sel: Omit<BetSlipSelection, "id" | "stake"> & { stake?: number }) => {
      const id = `${sel.marketId}:${sel.selectionId}:${sel.side}`;
      setSelections((prev) => {
        const existing = prev.find((s) => s.id === id);
        if (existing) {
          // Update price on re-click
          return prev.map((s) =>
            s.id === id ? { ...s, price: sel.price } : s,
          );
        }
        return [
          ...prev,
          {
            id,
            marketId: sel.marketId,
            marketName: sel.marketName,
            eventName: sel.eventName,
            selectionId: sel.selectionId,
            runnerName: sel.runnerName,
            side: sel.side,
            price: sel.price,
            stake: sel.stake ?? 0,
            isSession: sel.isSession,
          },
        ];
      });
      setIsOpen(true);
    },
    [],
  );

  const removeSelection = useCallback((id: string) => {
    setSelections((prev) => prev.filter((s) => s.id !== id));
  }, []);

  const updateSelection = useCallback(
    (id: string, patch: Partial<BetSlipSelection>) => {
      setSelections((prev) =>
        prev.map((s) => (s.id === id ? { ...s, ...patch } : s)),
      );
    },
    [],
  );

  // Sync `latestPrice` for any matching slip entries when the market feed
  // pushes new odds. We deliberately do NOT mutate `price` here — the user's
  // committed price stays put until they accept the move via acceptLivePrice.
  const syncLivePrices = useCallback(
    (marketId: string, runners: LiveRunnerPrice[]) => {
      setSelections((prev) => {
        let changed = false;
        const next = prev.map((s) => {
          if (s.marketId !== marketId) return s;
          const r = runners.find((x) => x.selectionId === s.selectionId);
          if (!r) return s;
          const newLive =
            s.side === "back" ? r.backPrice : r.layPrice;
          if (newLive == null || newLive <= 0) return s;
          if (s.latestPrice === newLive) return s;
          changed = true;
          let movement: "up" | "down" | undefined;
          if (newLive > s.price) movement = "up";
          else if (newLive < s.price) movement = "down";
          else movement = undefined;
          return { ...s, latestPrice: newLive, movement };
        });
        return changed ? next : prev;
      });
    },
    [],
  );

  const acceptLivePrice = useCallback((id: string) => {
    setSelections((prev) =>
      prev.map((s) =>
        s.id === id && s.latestPrice && s.latestPrice > 0
          ? { ...s, price: s.latestPrice, movement: undefined }
          : s,
      ),
    );
  }, []);

  const acceptAllLivePrices = useCallback(() => {
    setSelections((prev) =>
      prev.map((s) =>
        s.latestPrice && s.latestPrice > 0 && s.movement
          ? { ...s, price: s.latestPrice, movement: undefined }
          : s,
      ),
    );
  }, []);

  const clearAll = useCallback(() => setSelections([]), []);
  const open = useCallback(() => setIsOpen(true), []);
  const close = useCallback(() => setIsOpen(false), []);
  const toggle = useCallback(() => setIsOpen((v) => !v), []);

  // Auto-close on Escape
  useEffect(() => {
    if (!isOpen) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setIsOpen(false);
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [isOpen]);

  const value = useMemo<BetSlipContextValue>(
    () => ({
      selections,
      isOpen,
      open,
      close,
      toggle,
      addSelection,
      removeSelection,
      updateSelection,
      syncLivePrices,
      acceptLivePrice,
      acceptAllLivePrices,
      clearAll,
    }),
    [
      selections,
      isOpen,
      open,
      close,
      toggle,
      addSelection,
      removeSelection,
      updateSelection,
      syncLivePrices,
      acceptLivePrice,
      acceptAllLivePrices,
      clearAll,
    ],
  );

  return createElement(BetSlipContext.Provider, { value }, children);
}

export function useBetSlip(): BetSlipContextValue {
  const ctx = useContext(BetSlipContext);
  if (!ctx) {
    throw new Error("useBetSlip must be used inside <BetSlipProvider>");
  }
  return ctx;
}
