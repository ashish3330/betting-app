"use client";

import { useEffect, useState, useCallback, useRef, memo } from "react";
import { useParams } from "next/navigation";
import { api, MarketOdds, Runner, LiveScore as LiveScoreType, OrderBook as OrderBookType, OrderBookLevel } from "@/lib/api";
import { getWS } from "@/lib/ws";
import BetSlip, { BetSelection } from "@/components/BetSlip";
import LiveScore from "@/components/LiveScore";
import RunLadder from "@/components/RunLadder";

// ---------- Types ----------
interface PriceLevel {
  price: number;
  size: number;
}

type MarketTab = "match_odds" | "bookmaker" | "fancy" | "session";

// ---------- Types for multi-market ----------
interface MarketInfo {
  id: string;
  type: string;
  name: string;
}

// ---------- Main Page ----------
export default function MarketDetailPage() {
  const params = useParams();
  const marketId = params.id as string;

  const [odds, setOdds] = useState<MarketOdds | null>(null);
  const [orderBook, setOrderBook] = useState<OrderBookType>({ back: [], lay: [] });
  const [selections, setSelections] = useState<BetSelection[]>([]);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState<MarketTab>("match_odds");
  const [showMobileBetSlip, setShowMobileBetSlip] = useState(false);

  // All markets for this event (match_odds, bookmaker, fancy, etc.)
  const [eventMarkets, setEventMarkets] = useState<MarketInfo[]>([]);
  const [activeMarketId, setActiveMarketId] = useState<string>(marketId);
  const [liveScore, setLiveScore] = useState<LiveScoreType | null>(null);

  // User P&L positions per runner (selectionId -> P&L)
  const [positions, setPositions] = useState<Record<string, number>>({});

  // Track previous odds for flash animations
  const prevOddsRef = useRef<Record<string, { back: number; lay: number }>>({});

  // Load all markets for the event on first load
  useEffect(() => {
    async function loadEventMarkets() {
      try {
        // First get this market's data to find the event ID
        const data = await api.getMarketOdds(marketId);
        setOdds(data);

        // Extract event ID from market ID (convention: eventId-mo, eventId-bm, etc.)
        const eventId = marketId.replace(/-mo$|-bm$|-fancy\d*$|-ou$/, '');

        // Fetch all markets for this event
        const markets = await api.fetchEventMarkets(eventId);
        if (Array.isArray(markets) && markets.length > 0) {
          const infos: MarketInfo[] = markets.map(m => ({
            id: m.id,
            type: m.market_type || 'match_odds',
            name: m.name,
          }));
          setEventMarkets(infos);

          // Set active tab based on current market type
          const currentMarket = infos.find(m => m.id === marketId);
          if (currentMarket) {
            setActiveTab(currentMarket.type as MarketTab);
          }
        }
      } catch {
        // Fallback: just use the single market
        setEventMarkets([{ id: marketId, type: 'match_odds', name: '' }]);
      } finally {
        setLoading(false);
      }
    }
    loadEventMarkets();
  }, [marketId]);

  // Poll live scores every 10 seconds
  useEffect(() => {
    const eventId = marketId.replace(/-mo$|-bm$|-fancy\d*$|-ou$/, '');
    let mounted = true;
    async function fetchScore() {
      try {
        const score = await api.getLiveScore(eventId);
        if (mounted) setLiveScore(score);
      } catch {
        // No score available
      }
    }
    fetchScore();
    const interval = setInterval(fetchScore, 10000);
    return () => { mounted = false; clearInterval(interval); };
  }, [marketId]);

  // All markets of the active tab type (for fancy/session which can have multiple)
  const [allTabOdds, setAllTabOdds] = useState<{ market: MarketInfo; odds: MarketOdds }[]>([]);

  // When tab changes, switch market and load all markets of that type
  useEffect(() => {
    const marketsOfType = eventMarkets.filter(m => m.type === activeTab);
    if (marketsOfType.length > 0) {
      setActiveMarketId(marketsOfType[0].id);

      // For fancy/session, load ALL markets of this type
      if (activeTab === "fancy" || activeTab === "session") {
        Promise.all(
          marketsOfType.map(async (m) => {
            try {
              const data = await api.getMarketOdds(m.id);
              return { market: m, odds: data };
            } catch {
              return null;
            }
          })
        ).then((results) => {
          setAllTabOdds(results.filter(Boolean) as { market: MarketInfo; odds: MarketOdds }[]);
        });
      } else {
        setAllTabOdds([]);
      }
    }
  }, [activeTab, eventMarkets]);

  const loadData = useCallback(async () => {
    try {
      const [oddsData, obData] = await Promise.all([
        api.getMarketOdds(activeMarketId),
        api.getOrderBook(activeMarketId).catch(() => ({ back: [], lay: [] })),
      ]);
      // Store previous odds before updating
      if (odds?.runners) {
        const prev: Record<string, { back: number; lay: number }> = {};
        odds.runners.forEach((r) => {
          const rid = r.id || r.selection_id?.toString() || r.name;
          prev[rid] = {
            back: r.back_prices?.[0]?.price || r.back_price || 0,
            lay: r.lay_prices?.[0]?.price || r.lay_price || 0,
          };
        });
        prevOddsRef.current = prev;
      }
      setOdds(oddsData);
      setOrderBook(obData);
    } catch {
      // API unavailable
    }
  }, [activeMarketId, odds?.runners]);

  useEffect(() => {
    loadData();
    const interval = setInterval(loadData, 5000);

    const ws = getWS();
    ws.connect();
    ws.subscribe([activeMarketId]);

    const unsubOdds = ws.on("odds_update", (data: unknown) => {
      const update = data as MarketOdds;
      if (update.market_id === activeMarketId) {
        setOdds((prev) => {
          if (prev?.runners) {
            const prevMap: Record<string, { back: number; lay: number }> = {};
            prev.runners.forEach((r) => {
              const rid = r.id || r.selection_id?.toString() || r.name;
              prevMap[rid] = {
                back: r.back_prices?.[0]?.price || r.back_price || 0,
                lay: r.lay_prices?.[0]?.price || r.lay_price || 0,
              };
            });
            prevOddsRef.current = prevMap;
          }
          return update;
        });
      }
    });

    const unsubOB = ws.on("orderbook_update", (data: unknown) => {
      const update = data as { market_id: string } & OrderBookType;
      if (update.market_id === marketId) {
        setOrderBook({ back: update.back, lay: update.lay });
      }
    });

    return () => {
      clearInterval(interval);
      unsubOdds();
      unsubOB();
      ws.unsubscribe([activeMarketId]);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeMarketId]);

  // ---------- Fetch User Positions (P&L per runner) ----------
  const fetchPositions = useCallback(async () => {
    try {
      const pos = await api.getPositions(activeMarketId);
      setPositions(pos);
    } catch {
      // Not logged in or no positions
      setPositions({});
    }
  }, [activeMarketId]);

  useEffect(() => {
    fetchPositions();
    const interval = setInterval(fetchPositions, 5000);
    return () => clearInterval(interval);
  }, [fetchPositions]);

  // ---------- BetSlip Management ----------
  const handleSelectPrice = (runner: Runner, side: "back" | "lay", price: number) => {
    const selId = `${runner.id}_${side}_${Date.now()}`;
    const isSessionBet = isFancy && (runner.yes_rate != null || runner.no_rate != null);
    const newSelection: BetSelection = {
      id: selId,
      marketId: activeMarketId,
      runner,
      side,
      price,
      stake: 0,
      isSession: isSessionBet,
    };
    setSelections((prev) => [...prev, newSelection]);
    setShowMobileBetSlip(true);
  };

  const handleRemoveSelection = (id: string) => {
    setSelections((prev) => prev.filter((s) => s.id !== id));
    if (selections.length <= 1) setShowMobileBetSlip(false);
  };

  const handleClearAll = () => {
    setSelections([]);
    setShowMobileBetSlip(false);
  };

  const handleUpdateSelection = (id: string, updates: Partial<BetSelection>) => {
    setSelections((prev) =>
      prev.map((s) => (s.id === id ? { ...s, ...updates } : s))
    );
  };

  // Sync bet slip prices with live odds so the slip always shows current market price
  useEffect(() => {
    if (!odds?.runners || selections.length === 0) return;
    setSelections((prev) => {
      let changed = false;
      const next = prev.map((sel) => {
        // Only sync selections for the active market
        if (sel.marketId !== activeMarketId) return sel;
        const runner = odds.runners.find(
          (r) =>
            r.id === sel.runner.id ||
            (r.selection_id && r.selection_id === sel.runner.selection_id)
        );
        if (!runner) return sel;

        let livePrice: number | undefined;
        if (sel.isSession) {
          // Fancy/session: back uses yes_rate, lay uses no_rate
          livePrice = sel.side === "back" ? runner.yes_rate : runner.no_rate;
        } else if (sel.side === "back") {
          livePrice = runner.back_prices?.[0]?.price ?? runner.back_price;
        } else {
          livePrice = runner.lay_prices?.[0]?.price ?? runner.lay_price;
        }

        if (livePrice && livePrice !== sel.price) {
          changed = true;
          return { ...sel, price: livePrice };
        }
        return sel;
      });
      return changed ? next : prev;
    });
  }, [odds, activeMarketId]);

  const isSuspended = odds?.status === "suspended" || odds?.status === "closed";
  const currentMarketType = eventMarkets.find(m => m.id === activeMarketId)?.type || "match_odds";
  const isFancy = currentMarketType === "fancy" || currentMarketType === "session";
  const isBookmaker = currentMarketType === "bookmaker";

  // Only show tabs for market types that exist in this event
  const tabLabels: Record<string, string> = {
    match_odds: "Match Odds",
    bookmaker: "Bookmaker",
    fancy: "Fancy",
    session: "Session",
    over_under: "Over/Under",
  };
  const tabs = eventMarkets
    .map(m => ({ key: m.type as MarketTab, label: tabLabels[m.type] || m.type }))
    .filter((t, i, arr) => arr.findIndex(x => x.key === t.key) === i); // deduplicate

  // ---------- Loading Skeleton ----------
  if (loading) {
    return (
      <div className="flex min-h-[calc(100vh-56px)]">
        <div className="flex-1 max-w-[960px] mx-auto px-3 py-3 space-y-3">
          <div className="bg-surface rounded-lg h-16 animate-pulse" />
          <div className="bg-surface rounded-lg h-10 animate-pulse" />
          <div className="bg-surface rounded-lg h-64 animate-pulse" />
          <div className="bg-surface rounded-lg h-48 animate-pulse" />
        </div>
      </div>
    );
  }

  // ---------- Render ----------
  return (
    <div className="flex min-h-[calc(100vh-56px)]">
      {/* ===== Main Content ===== */}
      <div className="flex-1 min-w-0 pb-20 md:pb-0">
        <div className="max-w-[960px] mx-auto px-2 sm:px-3 py-2 space-y-2">

          {/* --- Match Header --- */}
          <div className="bg-surface rounded-lg border border-gray-800/60 px-3 py-2.5">
            <div className="flex items-center gap-2 flex-wrap">
              {odds?.in_play && (
                <span className="flex items-center gap-1.5 bg-profit/20 text-profit px-2.5 py-0.5 rounded text-[11px] font-semibold tracking-wide">
                  <span className="w-2 h-2 bg-profit rounded-full animate-live-pulse" />
                  LIVE
                </span>
              )}
              <span
                className={`text-[11px] px-2 py-0.5 rounded font-medium ${
                  odds?.status === "open" || odds?.status === "active"
                    ? "bg-profit/15 text-profit"
                    : odds?.status === "suspended"
                    ? "bg-yellow-500/15 text-yellow-400"
                    : "bg-gray-700 text-gray-400"
                }`}
              >
                {odds?.status?.toUpperCase() || "LOADING"}
              </span>
              {odds?.start_time && (
                <span className="text-[11px] text-gray-500">
                  {new Date(odds.start_time).toLocaleString("en-IN", {
                    day: "2-digit",
                    month: "short",
                    hour: "2-digit",
                    minute: "2-digit",
                  })}
                </span>
              )}
            </div>
            <h1 className="text-sm sm:text-base font-bold text-white mt-1 leading-tight">
              {odds?.event_name || `Market: ${marketId}`}
            </h1>
          </div>

          {/* --- Live Score --- */}
          {(liveScore || odds?.score) && <LiveScore score={liveScore || odds!.score!} />}

          {/* --- Market Tabs --- */}
          <div className="flex gap-0 bg-surface rounded-lg border border-gray-800/60 overflow-hidden">
            {tabs.map((tab) => (
              <button
                key={tab.key}
                onClick={() => setActiveTab(tab.key)}
                className={`flex-1 py-2 text-xs sm:text-[13px] font-semibold transition-colors ${
                  activeTab === tab.key
                    ? "bg-lotus text-white"
                    : "text-gray-400 hover:text-white hover:bg-surface-light"
                }`}
              >
                {tab.label}
              </button>
            ))}
          </div>

          {/* --- Odds Table --- */}
          <div className="bg-surface rounded-lg border border-gray-800/60 overflow-hidden">
            {/* Column Headers — different for each market type */}
            <div className="flex items-center h-8 border-b border-gray-800/40">
              <div className="flex-1 min-w-0 px-2 sm:px-3">
                <span className="text-[10px] text-gray-500 font-medium uppercase tracking-wider">
                  {isFancy ? "Session" : isBookmaker ? "Selection" : "Runner"}
                </span>
              </div>

              {isFancy ? (
                /* Fancy/Session: YES (back) + NO (lay) — 2 columns only */
                <div className="grid grid-cols-2 w-[110px] sm:w-[200px] flex-shrink-0">
                  <div className="text-center text-[10px] font-bold text-[#72bbef] bg-[#72bbef]/10 py-1.5">YES</div>
                  <div className="text-center text-[10px] font-bold text-[#faa9ba] bg-[#faa9ba]/10 py-1.5">NO</div>
                </div>
              ) : isBookmaker ? (
                /* Bookmaker: 1 Back + 1 Lay — 2 columns */
                <div className="grid grid-cols-2 w-[110px] sm:w-[240px] flex-shrink-0">
                  <div className="text-center text-[10px] font-bold text-[#72bbef] bg-[#72bbef]/10 py-1.5">Back</div>
                  <div className="text-center text-[10px] font-bold text-[#faa9ba] bg-[#faa9ba]/10 py-1.5">Lay</div>
                </div>
              ) : (
                /* Match Odds: 3 Back + 3 Lay */
                <div className="grid grid-cols-2 sm:grid-cols-6 w-[110px] sm:w-[396px] flex-shrink-0">
                  <div className="text-center text-[10px] font-semibold text-[#72bbef] bg-[#72bbef]/5 py-1.5 hidden sm:block">Back 3</div>
                  <div className="text-center text-[10px] font-semibold text-[#72bbef] bg-[#72bbef]/5 py-1.5 hidden sm:block">Back 2</div>
                  <div className="text-center text-[10px] font-bold text-[#72bbef] bg-[#72bbef]/10 py-1.5">Back</div>
                  <div className="text-center text-[10px] font-bold text-[#faa9ba] bg-[#faa9ba]/10 py-1.5">Lay</div>
                  <div className="text-center text-[10px] font-semibold text-[#faa9ba] bg-[#faa9ba]/5 py-1.5 hidden sm:block">Lay 2</div>
                  <div className="text-center text-[10px] font-semibold text-[#faa9ba] bg-[#faa9ba]/5 py-1.5 hidden sm:block">Lay 3</div>
                </div>
              )}
            </div>

            {/* Runner Rows */}
            <div className="relative">
              {/* SUSPENDED Overlay */}
              {isSuspended && (
                <div className="absolute inset-0 bg-black/60 z-10 flex items-center justify-center backdrop-blur-[1px]">
                  <span className="bg-red-600/90 text-white text-sm font-bold px-6 py-2 rounded tracking-wider">
                    SUSPENDED
                  </span>
                </div>
              )}

              {odds?.runners?.map((runner) => {
                const runnerId = runner.id || runner.selection_id?.toString() || runner.name;
                const isSelected = selections.some((s) => (s.runner.id || s.runner.selection_id?.toString()) === runnerId);
                const selectedSide = selections.find((s) => (s.runner.id || s.runner.selection_id?.toString()) === runnerId)?.side;
                return (
                  <RunnerRow
                    key={runnerId}
                    runner={{ ...runner, id: runnerId }}
                    prevOdds={prevOddsRef.current[runnerId]}
                    onSelect={handleSelectPrice}
                    isSelected={isSelected}
                    selectedSide={selectedSide}
                    marketType={currentMarketType}
                    pnl={positions[String(runner.selection_id)]}
                  />
                );
              })}

              {(!odds?.runners || odds.runners.length === 0) && (
                <div className="text-center py-10 text-gray-500 text-sm">
                  No runners available for this market
                </div>
              )}
            </div>
          </div>

          {/* --- Run Ladder only shows on click (not by default) --- */}

          {/* --- Extra Fancy/Session Markets (stacked list) --- */}
          {(isFancy) && allTabOdds.length > 0 && (
            <div className="space-y-1">
              {allTabOdds.map(({ market: m, odds: mOdds }) => (
                <FancyMarketBlock key={m.id} market={m} odds={mOdds} onSelect={handleSelectPrice} />
              ))}
            </div>
          )}

          {/* --- Order Book / Market Depth (only for match_odds) --- */}
          {!isFancy && <MarketDepth back={orderBook.back} lay={orderBook.lay} />}
        </div>
      </div>

      {/* ===== Desktop BetSlip Panel (Right) ===== */}
      <div className="hidden md:block w-[320px] flex-shrink-0 border-l border-gray-800/60">
        <div className="sticky top-14 h-[calc(100vh-56px)] overflow-y-auto p-3">
          <BetSlip
            selections={selections}
            onRemoveSelection={handleRemoveSelection}
            onClearAll={handleClearAll}
            onUpdateSelection={handleUpdateSelection}
            onBetPlaced={fetchPositions}
          />
        </div>
      </div>

      {/* ===== Mobile BetSlip Bottom Sheet ===== */}
      {showMobileBetSlip && selections.length > 0 && (
        <div className="md:hidden fixed inset-0 z-[60]">
          {/* Backdrop */}
          <div
            className="absolute inset-0 bg-black/60 backdrop-blur-sm"
            onClick={() => setShowMobileBetSlip(false)}
          />
          {/* Sheet — sits above bottom nav (56px) */}
          <div className="absolute bottom-0 left-0 right-0 bg-[var(--bg-surface)] rounded-t-2xl border-t border-gray-800 max-h-[80vh] overflow-y-auto animate-slide-up pb-20">
            {/* Drag Handle */}
            <div className="flex justify-center pt-2 pb-1 sticky top-0 bg-[var(--bg-surface)] z-10">
              <div className="w-10 h-1 rounded-full bg-gray-700" />
            </div>
            <div className="px-3 pb-4">
              <BetSlip
                selections={selections}
                onRemoveSelection={handleRemoveSelection}
                onClearAll={handleClearAll}
                onUpdateSelection={handleUpdateSelection}
              />
            </div>
          </div>
        </div>
      )}

      {/* Mobile floating bet count indicator */}
      {selections.length > 0 && !showMobileBetSlip && (
        <button
          onClick={() => setShowMobileBetSlip(true)}
          className="md:hidden fixed bottom-16 right-4 z-40 bg-lotus text-white rounded-full w-14 h-14 flex flex-col items-center justify-center shadow-lg shadow-lotus/20 active:scale-95 transition"
        >
          <span className="text-lg font-bold leading-none">{selections.length}</span>
          <span className="text-[8px] font-semibold">BETS</span>
        </button>
      )}
    </div>
  );
}

// ========== Runner Row ==========
function RunnerRow({
  runner,
  prevOdds,
  onSelect,
  isSelected,
  selectedSide,
  marketType = "match_odds",
  pnl,
}: {
  runner: Runner;
  prevOdds?: { back: number; lay: number };
  onSelect: (r: Runner, side: "back" | "lay", price: number) => void;
  isSelected: boolean;
  selectedSide?: "back" | "lay";
  marketType?: string;
  pnl?: number;
}) {
  const apiBackPrices = runner.back_prices || [];
  const apiLayPrices = runner.lay_prices || [];
  const isFancy = marketType === "fancy" || marketType === "session";
  const isBookmaker = marketType === "bookmaker";

  // Build price levels from API data
  const backLevels: PriceLevel[] = [
    apiBackPrices[2] || { price: 0, size: 0 },
    apiBackPrices[1] || { price: 0, size: 0 },
    apiBackPrices[0] || (runner.back_price ? { price: runner.back_price, size: runner.back_size || 0 } : { price: 0, size: 0 }),
  ];
  const layLevels: PriceLevel[] = [
    apiLayPrices[0] || (runner.lay_price ? { price: runner.lay_price, size: runner.lay_size || 0 } : { price: 0, size: 0 }),
    apiLayPrices[1] || { price: 0, size: 0 },
    apiLayPrices[2] || { price: 0, size: 0 },
  ];

  const bestBack = backLevels[2].price;
  const bestLay = layLevels[0].price;

  const backDirection = prevOdds && bestBack > 0 && prevOdds.back > 0
    ? bestBack > prevOdds.back ? "up" : bestBack < prevOdds.back ? "down" : null
    : null;
  const layDirection = prevOdds && bestLay > 0 && prevOdds.lay > 0
    ? bestLay > prevOdds.lay ? "up" : bestLay < prevOdds.lay ? "down" : null
    : null;

  const isRunnerSuspended = runner.status === "suspended";

  return (
    <div
      className={`flex items-center border-b border-gray-800/30 relative ${
        isSelected ? "bg-surface-light" : "hover:bg-surface-light/30"
      } transition-colors`}
    >
      {isRunnerSuspended && (
        <div className="absolute inset-0 bg-black/50 z-[5] flex items-center justify-center">
          <span className="text-[10px] font-bold text-yellow-400 uppercase tracking-wider">Suspended</span>
        </div>
      )}

      {/* Runner Name */}
      <div className="flex-1 min-w-0 px-2 sm:px-3 py-1.5">
        <div className="flex items-center gap-2">
          <span className="text-[12px] sm:text-[13px] font-semibold text-white block truncate">{runner.name}</span>
          {pnl != null && pnl !== 0 && (
            <span className={`text-[10px] sm:text-[11px] font-bold tabular-nums flex-shrink-0 ${pnl > 0 ? "text-green-400" : "text-red-400"}`}>
              {pnl > 0 ? "+" : ""}{formatPnl(pnl)}
            </span>
          )}
          {isFancy && runner.run_value != null && runner.run_value > 0 && (
            <span className="inline-flex items-center justify-center bg-lotus/20 text-lotus text-[11px] font-bold px-2 py-0.5 rounded-md min-w-[32px]">
              {runner.run_value}
            </span>
          )}
        </div>
      </div>

      {/* Odds Grid — layout changes by market type */}
      {isFancy && runner.yes_rate != null && runner.yes_rate > 0 ? (
        /* FANCY / SESSION with YES/NO rates */
        <div className="grid grid-cols-2 w-[110px] sm:w-[200px] flex-shrink-0">
          <button
            onClick={() => onSelect(runner, "back", runner.yes_rate || 0)}
            className="bg-[#72bbef] h-10 flex flex-col items-center justify-center border-r border-white/10 hover:brightness-110 active:brightness-95 transition-all cursor-pointer"
          >
            <span className="text-[13px] font-bold text-black">{runner.yes_rate || '-'}</span>
            <span className="text-[9px] text-black/60">YES</span>
          </button>
          <button
            onClick={() => onSelect(runner, "lay", runner.no_rate || 0)}
            className="bg-[#faa9ba] h-10 flex flex-col items-center justify-center hover:brightness-110 active:brightness-95 transition-all cursor-pointer"
          >
            <span className="text-[13px] font-bold text-black">{runner.no_rate || '-'}</span>
            <span className="text-[9px] text-black/60">NO</span>
          </button>
        </div>
      ) : isFancy ? (
        /* FANCY / SESSION fallback: 2 columns — YES (back) + NO (lay) */
        <div className="grid grid-cols-2 w-[110px] sm:w-[200px] flex-shrink-0">
          <OddsCell level={backLevels[2]} side="back" depth={0} runner={runner} onSelect={onSelect} direction={backDirection} isSelected={isSelected && selectedSide === "back"} />
          <OddsCell level={layLevels[0]} side="lay" depth={0} runner={runner} onSelect={onSelect} direction={layDirection} isSelected={isSelected && selectedSide === "lay"} />
        </div>
      ) : isBookmaker ? (
        /* BOOKMAKER: 2 columns — Back + Lay (wider cells, no depth) */
        <div className="grid grid-cols-2 w-[110px] sm:w-[240px] flex-shrink-0">
          <OddsCell level={backLevels[2]} side="back" depth={0} runner={runner} onSelect={onSelect} direction={backDirection} isSelected={isSelected && selectedSide === "back"} />
          <OddsCell level={layLevels[0]} side="lay" depth={0} runner={runner} onSelect={onSelect} direction={layDirection} isSelected={isSelected && selectedSide === "lay"} />
        </div>
      ) : (
        /* MATCH ODDS: 6 columns — 3 Back + 3 Lay */
        <div className="grid grid-cols-2 sm:grid-cols-6 w-[110px] sm:w-[396px] flex-shrink-0">
          <OddsCell level={backLevels[0]} side="back" depth={2} runner={runner} onSelect={onSelect} className="hidden sm:flex" />
          <OddsCell level={backLevels[1]} side="back" depth={1} runner={runner} onSelect={onSelect} className="hidden sm:flex" />
          <OddsCell level={backLevels[2]} side="back" depth={0} runner={runner} onSelect={onSelect} direction={backDirection} isSelected={isSelected && selectedSide === "back"} />
          <OddsCell level={layLevels[0]} side="lay" depth={0} runner={runner} onSelect={onSelect} direction={layDirection} isSelected={isSelected && selectedSide === "lay"} />
          <OddsCell level={layLevels[1]} side="lay" depth={1} runner={runner} onSelect={onSelect} className="hidden sm:flex" />
          <OddsCell level={layLevels[2]} side="lay" depth={2} runner={runner} onSelect={onSelect} className="hidden sm:flex" />
        </div>
      )}
    </div>
  );
}

// ========== Odds Cell (memoized — re-renders only when price/size/selection changes) ==========
const OddsCell = memo(function OddsCell({
  level,
  side,
  depth,
  runner,
  onSelect,
  direction,
  isSelected,
  className = "",
}: {
  level: PriceLevel;
  side: "back" | "lay";
  depth: 0 | 1 | 2;
  runner: Runner;
  onSelect: (r: Runner, side: "back" | "lay", price: number) => void;
  direction?: "up" | "down" | null;
  isSelected?: boolean;
  className?: string;
}) {
  // Back colors: depth 0 (best) = darkest, depth 2 = lightest
  // Lay colors: depth 0 (best) = darkest, depth 2 = lightest
  const backBgs = ["bg-[#72bbef]", "bg-[#a0d2f0]", "bg-[#b8e0f7]"];
  const layBgs = ["bg-[#faa9ba]", "bg-[#f7c3cf]", "bg-[#fad8e0]"];
  const bgClass = side === "back" ? backBgs[depth] : layBgs[depth];

  const hasPrice = level.price > 0;

  // Flash class based on direction
  const flashClass = direction === "up" ? "odds-up" : direction === "down" ? "odds-down" : "";

  return (
    <button
      onClick={() => hasPrice && onSelect(runner, side, level.price)}
      disabled={!hasPrice}
      className={`flex flex-col items-center justify-center h-10 border-r border-white/10 last:border-r-0 transition-all cursor-pointer relative
        ${bgClass} ${flashClass}
        ${isSelected ? "ring-2 ring-inset ring-yellow-400" : ""}
        ${hasPrice ? "hover:brightness-110 active:brightness-95" : "opacity-60"}
        ${className}
      `}
    >
      {hasPrice ? (
        <>
          <span className="text-[13px] sm:text-[14px] font-bold text-black leading-none">
            {level.price.toFixed(2)}
          </span>
          <span className="text-[10px] sm:text-[11px] text-black/60 leading-none mt-0.5">
            {formatSize(level.size)}
          </span>
          {direction && depth === 0 && (
            <span className={`absolute top-0.5 right-0.5 text-[8px] ${
              direction === "up" ? "text-green-700" : "text-red-700"
            }`}>
              {direction === "up" ? "\u25B2" : "\u25BC"}
            </span>
          )}
        </>
      ) : (
        <span className="text-[12px] text-black/30 font-medium">-</span>
      )}
    </button>
  );
});

// ========== Market Depth / Order Book ==========
function MarketDepth({ back, lay }: { back: OrderBookLevel[]; lay: OrderBookLevel[] }) {
  const backLevels = padLevels(back, 5);
  const layLevels = padLevels(lay, 5);
  const maxSize = Math.max(
    ...backLevels.map((l) => l.size || 0),
    ...layLevels.map((l) => l.size || 0),
    1
  );

  return (
    <div className="bg-surface rounded-lg border border-gray-800/60 overflow-hidden">
      <div className="px-3 py-2 border-b border-gray-800/40">
        <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider">
          Market Depth
        </h3>
      </div>

      {/* Column Headers */}
      <div className="grid grid-cols-4 text-center text-[10px] font-medium border-b border-gray-800/30 py-1.5">
        <div className="text-[#72bbef]">SIZE</div>
        <div className="text-[#72bbef]">BACK PRICE</div>
        <div className="text-[#faa9ba]">LAY PRICE</div>
        <div className="text-[#faa9ba]">SIZE</div>
      </div>

      {/* Depth Levels */}
      <div className="px-1.5 py-1.5">
        {Array.from({ length: 5 }).map((_, i) => {
          const bk = backLevels[i];
          const ly = layLevels[i];
          return (
            <div key={i} className="grid grid-cols-4 gap-0.5 mb-0.5">
              {/* Back Size Bar */}
              <div className="relative bg-[#72bbef]/5 rounded-l px-2 py-1.5 text-right overflow-hidden">
                <div
                  className="absolute top-0 bottom-0 right-0 bg-[#72bbef]/20 rounded-l"
                  style={{ width: bk.size ? `${(bk.size / maxSize) * 100}%` : "0%" }}
                />
                <span className="relative text-xs text-[#72bbef] font-mono">
                  {bk.size ? formatSize(bk.size) : "-"}
                </span>
              </div>
              {/* Back Price */}
              <div className="bg-[#72bbef]/15 px-2 py-1.5 text-center">
                <span className="text-xs font-bold text-[#72bbef]">
                  {bk.price ? bk.price.toFixed(2) : "-"}
                </span>
              </div>
              {/* Lay Price */}
              <div className="bg-[#faa9ba]/15 px-2 py-1.5 text-center">
                <span className="text-xs font-bold text-[#faa9ba]">
                  {ly.price ? ly.price.toFixed(2) : "-"}
                </span>
              </div>
              {/* Lay Size Bar */}
              <div className="relative bg-[#faa9ba]/5 rounded-r px-2 py-1.5 text-left overflow-hidden">
                <div
                  className="absolute top-0 bottom-0 left-0 bg-[#faa9ba]/20 rounded-r"
                  style={{ width: ly.size ? `${(ly.size / maxSize) * 100}%` : "0%" }}
                />
                <span className="relative text-xs text-[#faa9ba] font-mono">
                  {ly.size ? formatSize(ly.size) : "-"}
                </span>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ========== Fancy Market Block (with toggleable run ladder) ==========
function FancyMarketBlock({ market, odds, onSelect }: {
  market: { id: string; name: string };
  odds: MarketOdds | null;
  onSelect: (r: Runner, side: "back" | "lay", price: number) => void;
}) {
  const [showLadder, setShowLadder] = useState(false);

  return (
    <div className="bg-surface rounded-lg border border-gray-800/60 overflow-hidden">
      <button
        onClick={() => setShowLadder(!showLadder)}
        className="w-full px-2 py-1.5 border-b border-gray-800/40 bg-surface-light/30 flex items-center justify-between hover:bg-surface-light/50 transition"
      >
        <span className="text-[11px] font-medium text-gray-300">{market.name}</span>
        <svg className={`w-3 h-3 text-gray-500 transition-transform ${showLadder ? "rotate-180" : ""}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </button>
      {odds?.runners?.map((runner) => {
        const rid = runner.id || runner.selection_id?.toString() || runner.name;
        return (
          <RunnerRow
            key={rid}
            runner={{ ...runner, id: rid }}
            onSelect={onSelect}
            isSelected={false}
            marketType="fancy"
          />
        );
      })}
      {showLadder && <RunLadder marketId={market.id} />}
    </div>
  );
}

// ========== Helpers ==========
function padLevels(levels: OrderBookLevel[], count: number): OrderBookLevel[] {
  const padded = [...levels];
  while (padded.length < count) {
    padded.push({ price: 0, size: 0 });
  }
  return padded.slice(0, count);
}

function formatSize(size: number): string {
  if (size >= 100000) return `${(size / 100000).toFixed(1)}L`;
  if (size >= 1000) return `${(size / 1000).toFixed(1)}K`;
  return size.toFixed(0);
}

function formatPnl(n: number): string {
  const abs = Math.abs(n);
  const sign = n < 0 ? "-" : "";
  if (abs >= 10000000) return `${sign}\u20B9${(abs / 10000000).toFixed(1)}Cr`;
  if (abs >= 100000) return `${sign}\u20B9${(abs / 100000).toFixed(1)}L`;
  if (abs >= 1000) return `${sign}\u20B9${(abs / 1000).toFixed(0)}K`;
  return `${sign}\u20B9${abs.toFixed(0)}`;
}
