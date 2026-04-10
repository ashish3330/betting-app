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
  price: number;
  stake: number;
  /** True for session/fancy bets (profit = stake * rate / 100 instead of odds). */
  isSession?: boolean;
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
