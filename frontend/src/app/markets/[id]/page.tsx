"use client";

import { useEffect, useState, useCallback, useRef, memo } from "react";
import { useParams } from "next/navigation";
import { api, MarketOdds, Runner, LiveScore as LiveScoreType, OrderBook as OrderBookType, OrderBookLevel } from "@/lib/api";
import { getWS } from "@/lib/ws";
import LiveScore from "@/components/LiveScore";
import RunLadder from "@/components/RunLadder";
import { useBetSlip } from "@/lib/betslip";
import { SkeletonCardRow } from "@/components/Skeleton";

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
  const {
    addSelection: pushToGlobalSlip,
    selections: slipSelections,
    syncLivePrices,
  } = useBetSlip();

  const [odds, setOdds] = useState<MarketOdds | null>(null);
  const [orderBook, setOrderBook] = useState<OrderBookType>({ back: [], lay: [] });
  // First-load skeleton only — subsequent polls update silently
  const [initialLoading, setInitialLoading] = useState(true);
  const [activeTab, setActiveTab] = useState<MarketTab>("match_odds");

  // Bets fetched from API for this market
  const [marketBets, setMarketBets] = useState<{id:string, side:string, display_side?:string, market_type?:string, price:number, stake:number, status:string, created_at:string, selection_name?:string, market_name?:string}[]>([]);

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
        setInitialLoading(false);
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
      // Sync any open bet slip selections to the latest live prices.
      if (oddsData?.runners) {
        const live = oddsData.runners.map((r) => ({
          selectionId: r.selection_id,
          backPrice: r.back_prices?.[0]?.price ?? r.back_price ?? 0,
          layPrice: r.lay_prices?.[0]?.price ?? r.lay_price ?? 0,
        }));
        syncLivePrices(activeMarketId, live);
      }
    } catch {
      // API unavailable
    }
  }, [activeMarketId, odds?.runners, syncLivePrices]);

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
        // Push the latest live prices into any bet slip selections that
        // reference this market — keeps the slip in sync with live odds.
        if (update.runners) {
          const live = update.runners.map((r) => ({
            selectionId: r.selection_id,
            backPrice: r.back_prices?.[0]?.price ?? r.back_price ?? 0,
            layPrice: r.lay_prices?.[0]?.price ?? r.lay_price ?? 0,
          }));
          syncLivePrices(activeMarketId, live);
        }
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

  // ---------- Fetch Market Bets from API ----------
  useEffect(() => {
    async function loadMarketBets() {
      try {
        const data = await api.request<{bets: typeof marketBets}>(`/api/v1/bets?market_id=${activeMarketId}&limit=20`, { auth: true });
        setMarketBets(data.bets || []);
      } catch {}
    }
    if (activeMarketId) loadMarketBets();
    const interval = setInterval(loadMarketBets, 10000);
    return () => clearInterval(interval);
  }, [activeMarketId]);

  // Derived market type (needed before handlers)
  const currentMarketType = eventMarkets.find(m => m.id === activeMarketId)?.type || "match_odds";
  const isFancy = currentMarketType === "fancy" || currentMarketType === "session";
  const isBookmaker = currentMarketType === "bookmaker";
  const isSuspended = odds?.status === "suspended" || odds?.status === "closed";

  // ---------- Global BetSlip Management ----------
  // Clicking a back/lay price pushes the selection into the global persistent
  // bet slip drawer (BetSlipProvider). There is no inline bet slip per row.
  const handleSelectPrice = useCallback(
    (runner: Runner, side: "back" | "lay", price: number) => {
      if (!price || price <= 0) return;
      const isSessionBet = isFancy && (runner.yes_rate != null || runner.no_rate != null);
      pushToGlobalSlip({
        marketId: activeMarketId,
        marketName: eventMarkets.find((m) => m.id === activeMarketId)?.name,
        eventName: odds?.event_name,
        selectionId: runner.selection_id || 0,
        runnerName: runner.name,
        side,
        price,
        isSession: isSessionBet,
      });
    },
    [activeMarketId, eventMarkets, odds?.event_name, isFancy, pushToGlobalSlip],
  );

  // Build a quick lookup of selections currently in the global slip so that
  // cells belonging to those selections can render a subtle visual cue.
  const slipKeys = new Set(
    slipSelections
      .filter((s) => s.marketId === activeMarketId)
      .map((s) => `${s.selectionId}:${s.side}`),
  );


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

  // ---------- First-Load Skeleton (only shown on initial load, not on polls) ----------
  if (initialLoading && !odds) {
    return (
      <div className="flex min-h-[calc(100vh-56px)]">
        <div className="flex-1 max-w-[960px] mx-auto px-3 py-3 space-y-3">
          <SkeletonCardRow count={3} />
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
                <span className="flex items-center gap-1.5 bg-red-500/20 text-red-400 px-2.5 py-0.5 rounded text-[11px] font-bold tracking-wide animate-pulse">
                  <span className="w-2 h-2 bg-red-500 rounded-full" />
                  LIVE
                </span>
              )}
              {odds?.in_play && (liveScore || odds?.score) && (() => {
                const s = liveScore || odds!.score!;
                return (
                  <span className="text-[11px] text-gray-300 font-mono tracking-tight">
                    {s.home_score}
                    {s.overs ? ` (${s.overs} ov)` : ""}
                  </span>
                );
              })()}
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

          {/* --- Market Type Badge --- */}
          <div className="bg-surface rounded-lg border border-gray-800/60 px-3 py-1.5 flex items-center gap-3 sm:gap-4 flex-wrap">
            <span className={`text-[10px] font-semibold px-2 py-0.5 rounded ml-auto ${
              currentMarketType === 'bookmaker' ? 'bg-yellow-500/15 text-yellow-400' :
              currentMarketType === 'fancy' || currentMarketType === 'session' ? 'bg-purple-500/15 text-purple-400' :
              'bg-blue-500/15 text-blue-400'
            }`}>
              {currentMarketType === 'bookmaker' ? 'Bookmaker' :
               currentMarketType === 'fancy' || currentMarketType === 'session' ? 'Fancy' :
               'Match Odds'}
            </span>
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
                /* Match Odds: 3 Back + 3 Lay — always 6 columns, compact on mobile */
                <div className="grid grid-cols-6 w-[228px] sm:w-[396px] flex-shrink-0">
                  <div className="text-center text-[9px] sm:text-[10px] font-semibold text-[#72bbef] bg-[#72bbef]/5 py-1.5">B3</div>
                  <div className="text-center text-[9px] sm:text-[10px] font-semibold text-[#72bbef] bg-[#72bbef]/5 py-1.5">B2</div>
                  <div className="text-center text-[9px] sm:text-[10px] font-bold text-[#72bbef] bg-[#72bbef]/10 py-1.5">Back</div>
                  <div className="text-center text-[9px] sm:text-[10px] font-bold text-[#faa9ba] bg-[#faa9ba]/10 py-1.5">Lay</div>
                  <div className="text-center text-[9px] sm:text-[10px] font-semibold text-[#faa9ba] bg-[#faa9ba]/5 py-1.5">L2</div>
                  <div className="text-center text-[9px] sm:text-[10px] font-semibold text-[#faa9ba] bg-[#faa9ba]/5 py-1.5">L3</div>
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
                const selId = runner.selection_id || 0;
                return (
                  <RunnerRow
                    key={runnerId}
                    runner={{ ...runner, id: runnerId }}
                    prevOdds={prevOddsRef.current[runnerId]}
                    onSelect={handleSelectPrice}
                    backInSlip={slipKeys.has(`${selId}:back`)}
                    layInSlip={slipKeys.has(`${selId}:lay`)}
                    marketType={currentMarketType}
                    pnl={positions[String(runner.selection_id)]}
                    allPositions={positions}
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

      {/* ===== Desktop My Bets Panel (Right) ===== */}
      <div className="hidden md:block w-[320px] flex-shrink-0 border-l border-gray-800/60">
        <div className="sticky top-14 h-[calc(100vh-56px)] overflow-y-auto p-3 space-y-3">
          {/* My Bets on Market (from API — single source of truth) */}
          <div className="bg-surface rounded-lg border border-gray-800/60 overflow-hidden">
            <div className="px-3 py-2.5 border-b border-gray-800/40">
              <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider">My Bets on Market</h3>
            </div>
            {marketBets.length === 0 ? (
              <div className="px-3 py-8 text-center text-gray-500 text-xs">
                No bets placed yet. Click on any odds to add to the bet slip.
              </div>
            ) : (
              <div className="divide-y divide-gray-800/30">
                {marketBets.map((bet) => {
                  const betIsFancy = bet.market_type === "fancy" || bet.market_type === "session";
                  const ds = bet.display_side || bet.side;
                  return (
                  <div key={bet.id} className="px-3 py-2">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-1.5 min-w-0">
                        {betIsFancy && <span className="text-[8px] font-bold px-1 py-0.5 rounded bg-purple-500/20 text-purple-400">FANCY</span>}
                        <span className="text-[12px] font-medium text-white truncate">{bet.selection_name || 'Selection'}</span>
                      </div>
                      <span className={`text-[10px] font-bold px-1.5 py-0.5 rounded ${
                        bet.status === 'matched' ? 'bg-green-500/20 text-green-400' : 'bg-gray-500/20 text-gray-400'
                      }`}>{bet.status === 'matched' ? 'PLACED' : bet.status.toUpperCase()}</span>
                    </div>
                    <div className="flex items-center gap-2 mt-1">
                      <span className={`text-[10px] font-bold px-1.5 py-0.5 rounded ${
                        ds === 'back' || ds === 'yes' ? 'bg-[#72BBEF]/20 text-[#72BBEF]' : 'bg-[#FAA9BA]/20 text-[#FAA9BA]'
                      }`}>{ds.toUpperCase()}</span>
                      <span className="text-[11px] text-gray-400">@ {bet.price.toFixed(2)}</span>
                      <span className="text-[11px] text-gray-300 font-medium">{'\u20B9'}{bet.stake.toLocaleString('en-IN')}</span>
                      <span className="text-[10px] text-gray-600 ml-auto">
                        {new Date(bet.created_at).toLocaleTimeString('en-IN', { hour: '2-digit', minute: '2-digit' })}
                      </span>
                    </div>
                    <div className="text-[9px] text-gray-600 mt-0.5">ID: {bet.id}</div>
                  </div>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// ========== Runner Row (pushes selections to the global BetSlipProvider) ==========
function RunnerRow({
  runner,
  prevOdds,
  onSelect,
  backInSlip,
  layInSlip,
  marketType = "match_odds",
  pnl,
  allPositions,
}: {
  runner: Runner;
  prevOdds?: { back: number; lay: number };
  onSelect: (r: Runner, side: "back" | "lay", price: number) => void;
  backInSlip?: boolean;
  layInSlip?: boolean;
  marketType?: string;
  pnl?: number;
  allPositions?: Record<string, number>;
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
    <div className="border-b border-gray-800/30">
      <div className="flex items-center relative hover:bg-surface-light/30 transition-colors">
        {isRunnerSuspended && (
          <div className="absolute inset-0 bg-black/50 z-[5] flex items-center justify-center">
            <span className="text-[10px] font-bold text-yellow-400 uppercase tracking-wider">Suspended</span>
          </div>
        )}

        {/* Runner Name + P&L */}
        <div className="flex-1 min-w-0 px-2 sm:px-3 py-1.5">
          <div className="flex items-center gap-2">
            <span className="text-[12px] sm:text-[13px] font-semibold text-white block truncate">{runner.name}</span>
            {pnl != null && pnl !== 0 && (
              <span className={`text-[11px] sm:text-[12px] font-bold tabular-nums flex-shrink-0 px-1.5 py-0.5 rounded ${
                pnl > 0 ? "text-green-400 bg-green-500/10" : "text-red-400 bg-red-500/10"
              }`}>
                {pnl > 0 ? "+" : ""}{formatPnl(pnl)}
              </span>
            )}
            {isFancy && runner.run_value != null && runner.run_value > 0 && (
              <span className="inline-flex items-center justify-center bg-lotus/20 text-lotus text-[11px] font-bold px-2 py-0.5 rounded-md min-w-[32px]">
                {runner.run_value}
              </span>
            )}
          </div>
          {/* Book P&L row — show positions across all runners when user has any */}
          {allPositions && Object.values(allPositions).some(v => v !== 0) && (
            <div className="flex items-center gap-2 mt-0.5">
              <span className="text-[9px] text-gray-600 uppercase">Book P&L:</span>
              {pnl != null && pnl !== 0 ? (
                <span className={`text-[10px] font-bold tabular-nums ${pnl > 0 ? "text-green-400" : "text-red-400"}`}>
                  {pnl > 0 ? "+" : ""}{'\u20B9'}{Math.abs(pnl).toLocaleString('en-IN')}
                </span>
              ) : (
                <span className="text-[10px] text-gray-600 tabular-nums">{'\u20B9'}0</span>
              )}
            </div>
          )}
        </div>

        {/* Odds Grid -- layout changes by market type */}
        {isFancy && runner.yes_rate != null && runner.yes_rate > 0 ? (
          /* FANCY / SESSION with YES/NO rates */
          <div className="grid grid-cols-2 w-[110px] sm:w-[200px] flex-shrink-0">
            <button
              onClick={() => onSelect(runner, "back", runner.yes_rate || 0)}
              className={`bg-[#72bbef] h-10 flex flex-col items-center justify-center border-r border-white/10 hover:brightness-110 active:brightness-95 transition-all cursor-pointer ${backInSlip ? 'ring-1 ring-inset ring-white/70' : ''}`}
            >
              <span className="text-[13px] font-bold text-black">{runner.yes_rate || '-'}</span>
              <span className="text-[9px] text-black/60">YES</span>
            </button>
            <button
              onClick={() => onSelect(runner, "lay", runner.no_rate || 0)}
              className={`bg-[#faa9ba] h-10 flex flex-col items-center justify-center hover:brightness-110 active:brightness-95 transition-all cursor-pointer ${layInSlip ? 'ring-1 ring-inset ring-white/70' : ''}`}
            >
              <span className="text-[13px] font-bold text-black">{runner.no_rate || '-'}</span>
              <span className="text-[9px] text-black/60">NO</span>
            </button>
          </div>
        ) : isFancy ? (
          /* FANCY / SESSION fallback: 2 columns -- YES (back) + NO (lay) */
          <div className="grid grid-cols-2 w-[110px] sm:w-[200px] flex-shrink-0">
            <OddsCell level={backLevels[2]} side="back" depth={0} runner={runner} onSelect={onSelect} direction={backDirection} isSelected={backInSlip} />
            <OddsCell level={layLevels[0]} side="lay" depth={0} runner={runner} onSelect={onSelect} direction={layDirection} isSelected={layInSlip} />
          </div>
        ) : isBookmaker ? (
          /* BOOKMAKER: 2 columns -- Back + Lay (wider cells, no depth) */
          <div className="grid grid-cols-2 w-[110px] sm:w-[240px] flex-shrink-0">
            <OddsCell level={backLevels[2]} side="back" depth={0} runner={runner} onSelect={onSelect} direction={backDirection} isSelected={backInSlip} />
            <OddsCell level={layLevels[0]} side="lay" depth={0} runner={runner} onSelect={onSelect} direction={layDirection} isSelected={layInSlip} />
          </div>
        ) : (
          /* MATCH ODDS: 6 columns always visible -- 3 Back + 3 Lay */
          <div className="grid grid-cols-6 w-[228px] sm:w-[396px] flex-shrink-0">
            <OddsCell level={backLevels[0]} side="back" depth={2} runner={runner} onSelect={onSelect} compact />
            <OddsCell level={backLevels[1]} side="back" depth={1} runner={runner} onSelect={onSelect} compact />
            <OddsCell level={backLevels[2]} side="back" depth={0} runner={runner} onSelect={onSelect} direction={backDirection} isSelected={backInSlip} />
            <OddsCell level={layLevels[0]} side="lay" depth={0} runner={runner} onSelect={onSelect} direction={layDirection} isSelected={layInSlip} />
            <OddsCell level={layLevels[1]} side="lay" depth={1} runner={runner} onSelect={onSelect} compact />
            <OddsCell level={layLevels[2]} side="lay" depth={2} runner={runner} onSelect={onSelect} compact />
          </div>
        )}
      </div>
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
  compact = false,
}: {
  level: PriceLevel;
  side: "back" | "lay";
  depth: 0 | 1 | 2;
  runner: Runner;
  onSelect: (r: Runner, side: "back" | "lay", price: number) => void;
  direction?: "up" | "down" | null;
  isSelected?: boolean;
  /** Compact mode for secondary depth cells on mobile. */
  compact?: boolean;
}) {
  // Back colors: depth 0 (best) = darkest, depth 2 = lightest
  const backBgs = ["bg-[#72bbef]", "bg-[#a0d2f0]", "bg-[#b8e0f7]"];
  const layBgs = ["bg-[#faa9ba]", "bg-[#f7c3cf]", "bg-[#fad8e0]"];
  const bgClass = side === "back" ? backBgs[depth] : layBgs[depth];

  const hasPrice = level.price > 0;

  // Full-cell flash animation based on price direction change. A fresh key
  // forces the animation to replay every time the direction toggles.
  const [flashKey, setFlashKey] = useState(0);
  const lastDirRef = useRef<"up" | "down" | null | undefined>(null);
  useEffect(() => {
    if (direction && direction !== lastDirRef.current) {
      setFlashKey((k) => k + 1);
    }
    lastDirRef.current = direction;
  }, [direction, level.price]);
  const flashClass =
    direction === "up"
      ? "animate-flash-up"
      : direction === "down"
      ? "animate-flash-down"
      : "";

  const priceText = compact ? "text-[10px] sm:text-[14px]" : "text-[12px] sm:text-[14px]";
  const sizeText = compact ? "text-[8px] sm:text-[11px]" : "text-[9px] sm:text-[11px]";

  return (
    <button
      onClick={() => hasPrice && onSelect(runner, side, level.price)}
      disabled={!hasPrice}
      className={`flex flex-col items-center justify-center h-10 border-r border-white/10 last:border-r-0 transition-all cursor-pointer relative overflow-hidden
        ${bgClass}
        ${isSelected ? "ring-1 ring-inset ring-white/80" : ""}
        ${hasPrice ? "hover:brightness-110 active:brightness-95" : "opacity-60"}
      `}
    >
      {/* Flash overlay — a separate layer so it doesn't fight the base back/lay bg */}
      {flashClass && (
        <span
          key={flashKey}
          className={`pointer-events-none absolute inset-0 ${flashClass}`}
          aria-hidden
        />
      )}
      {hasPrice ? (
        <>
          <span className={`${priceText} font-bold text-black leading-none relative`}>
            {level.price.toFixed(2)}
          </span>
          <span className={`${sizeText} text-black/60 leading-none mt-0.5 relative`}>
            {formatSize(level.size)}
          </span>
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
  return (
    <div className="bg-surface rounded-lg border border-gray-800/60 overflow-hidden">
      <div className="w-full px-2 py-1.5 border-b border-gray-800/40 bg-surface-light/30 flex items-center justify-between">
        <span className="text-[11px] font-medium text-gray-300">{market.name}</span>
      </div>
      {odds?.runners?.map((runner) => {
        const rid = runner.id || runner.selection_id?.toString() || runner.name;
        return (
          <RunnerRow
            key={rid}
            runner={{ ...runner, id: rid }}
            onSelect={onSelect}
            marketType="fancy"
          />
        );
      })}
      <RunLadder marketId={market.id} />
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
  // Show exact amount — no rounding to K that loses precision
  return `${sign}\u20B9${abs.toLocaleString("en-IN", { maximumFractionDigits: 0 })}`;
}
