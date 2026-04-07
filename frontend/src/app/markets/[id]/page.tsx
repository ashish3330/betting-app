"use client";

import { useEffect, useState, useCallback, useRef, memo } from "react";
import { useParams } from "next/navigation";
import { api, MarketOdds, Runner, LiveScore as LiveScoreType, OrderBook as OrderBookType, OrderBookLevel } from "@/lib/api";
import { getWS } from "@/lib/ws";
// BetSlip component no longer used directly -- inline bet slip handles bet entry
import LiveScore from "@/components/LiveScore";
import RunLadder from "@/components/RunLadder";
import { useAuth } from "@/lib/auth";
import { useToast } from "@/components/Toast";

// ---------- Inline Bet Slip Types ----------
interface ActiveInlineBet {
  runnerId: string;
  runner: Runner;
  side: "back" | "lay";
  price: number;
  marketId: string;
  isSession?: boolean;
}

interface PlacedBetResult {
  runnerId: string;
  status: "matched" | "error";
  message: string;
}

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
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState<MarketTab>("match_odds");

  // Inline bet slip state — only one active at a time
  const [activeInlineBet, setActiveInlineBet] = useState<ActiveInlineBet | null>(null);
  const [placedBetResult, setPlacedBetResult] = useState<PlacedBetResult | null>(null);

  // Recently placed bets for "My Bets" panel
  const [recentBets, setRecentBets] = useState<Array<{
    id: string;
    runnerName: string;
    side: "back" | "lay";
    price: number;
    stake: number;
    status: string;
    timestamp: number;
  }>>([]);

  // Bets fetched from API for this market
  const [marketBets, setMarketBets] = useState<{id:string, side:string, price:number, stake:number, status:string, created_at:string, selection_name?:string, market_name?:string}[]>([]);

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

  // ---------- Inline BetSlip Management ----------
  const handleSelectPrice = (runner: Runner, side: "back" | "lay", price: number) => {
    const runnerId = runner.id || runner.selection_id?.toString() || runner.name;
    const isSessionBet = isFancy && (runner.yes_rate != null || runner.no_rate != null);

    // If same runner+side is already selected, deselect it
    if (activeInlineBet && activeInlineBet.runnerId === runnerId && activeInlineBet.side === side) {
      setActiveInlineBet(null);
      return;
    }

    // Set the inline bet (only one at a time)
    setActiveInlineBet({
      runnerId,
      runner,
      side,
      price,
      marketId: activeMarketId,
      isSession: isSessionBet,
    });
    // Clear any previous placed bet result
    setPlacedBetResult(null);
  };

  const handleDeselectInline = () => {
    setActiveInlineBet(null);
    setPlacedBetResult(null);
  };

  const handleBetPlaced = (result: PlacedBetResult, runnerName: string, side: "back" | "lay", price: number, stake: number) => {
    setPlacedBetResult(result);
    // Add to recent bets
    setRecentBets((prev) => [{
      id: `${Date.now()}`,
      runnerName,
      side,
      price,
      stake,
      status: result.status,
      timestamp: Date.now(),
    }, ...prev].slice(0, 20)); // keep last 20

    // Auto-close inline slip after 3 seconds
    setTimeout(() => {
      setActiveInlineBet(null);
      setPlacedBetResult(null);
    }, 3000);

    // Refresh positions
    fetchPositions();
  };


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

          {/* --- Market Info Bar --- */}
          <div className="bg-surface rounded-lg border border-gray-800/60 px-3 py-1.5 flex items-center gap-3 sm:gap-4 flex-wrap">
            <span className="text-[10px] text-gray-500">Min Bet: <span className="text-gray-400 font-medium">{'\u20B9'}100</span></span>
            <span className="text-[10px] text-gray-500">Max Bet: <span className="text-gray-400 font-medium">{'\u20B9'}5,00,000</span></span>
            <span className="text-[10px] text-gray-500">Max Market: <span className="text-gray-400 font-medium">{'\u20B9'}25,00,000</span></span>
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
                const isInlineSelected = activeInlineBet?.runnerId === runnerId;
                const inlineSide = isInlineSelected ? activeInlineBet!.side : undefined;
                return (
                  <RunnerRow
                    key={runnerId}
                    runner={{ ...runner, id: runnerId }}
                    prevOdds={prevOddsRef.current[runnerId]}
                    onSelect={handleSelectPrice}
                    isSelected={isInlineSelected}
                    selectedSide={inlineSide}
                    marketType={currentMarketType}
                    pnl={positions[String(runner.selection_id)]}
                    allPositions={positions}
                    activeInlineBet={isInlineSelected ? activeInlineBet! : undefined}
                    placedBetResult={isInlineSelected ? placedBetResult : undefined}
                    onDeselect={handleDeselectInline}
                    onBetPlaced={handleBetPlaced}
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
          {/* Session Bets (placed this session) */}
          {recentBets.length > 0 && (
            <div className="bg-surface rounded-lg border border-gray-800/60 overflow-hidden">
              <div className="px-3 py-2.5 border-b border-gray-800/40">
                <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider">Session Bets</h3>
              </div>
              <div className="divide-y divide-gray-800/30">
                {recentBets.map((bet) => (
                  <div key={bet.id} className="px-3 py-2">
                    <div className="flex items-center justify-between">
                      <span className="text-[12px] font-medium text-white truncate">{bet.runnerName}</span>
                      <span className={`text-[10px] font-bold px-1.5 py-0.5 rounded ${
                        bet.status === 'matched' ? 'bg-green-500/20 text-green-400' : 'bg-red-500/20 text-red-400'
                      }`}>{bet.status.toUpperCase()}</span>
                    </div>
                    <div className="flex items-center gap-2 mt-1">
                      <span className={`text-[10px] font-bold px-1.5 py-0.5 rounded ${
                        bet.side === 'back' ? 'bg-[#72BBEF]/20 text-[#72BBEF]' : 'bg-[#FAA9BA]/20 text-[#FAA9BA]'
                      }`}>{bet.side.toUpperCase()}</span>
                      <span className="text-[11px] text-gray-400">@ {bet.price.toFixed(2)}</span>
                      <span className="text-[11px] text-gray-300 font-medium">{'\u20B9'}{bet.stake.toLocaleString('en-IN')}</span>
                      <span className="text-[10px] text-gray-600 ml-auto">
                        {new Date(bet.timestamp).toLocaleTimeString('en-IN', { hour: '2-digit', minute: '2-digit' })}
                      </span>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Market Bets (from API) */}
          <div className="bg-surface rounded-lg border border-gray-800/60 overflow-hidden">
            <div className="px-3 py-2.5 border-b border-gray-800/40">
              <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider">My Bets on Market</h3>
            </div>
            {marketBets.length === 0 && recentBets.length === 0 ? (
              <div className="px-3 py-8 text-center text-gray-500 text-xs">
                No bets placed yet. Click on odds to place a bet inline.
              </div>
            ) : marketBets.length === 0 ? (
              <div className="px-3 py-4 text-center text-gray-600 text-[10px]">
                No bet history loaded for this market.
              </div>
            ) : (
              <div className="divide-y divide-gray-800/30">
                {marketBets.map((bet) => (
                  <div key={bet.id} className="px-3 py-2">
                    <div className="flex items-center justify-between">
                      <span className="text-[12px] font-medium text-white truncate">{bet.selection_name || 'Selection'}</span>
                      <span className={`text-[10px] font-bold px-1.5 py-0.5 rounded ${
                        bet.status === 'matched' ? 'bg-green-500/20 text-green-400' :
                        bet.status === 'pending' ? 'bg-yellow-500/20 text-yellow-400' :
                        'bg-red-500/20 text-red-400'
                      }`}>{bet.status.toUpperCase()}</span>
                    </div>
                    <div className="flex items-center gap-2 mt-1">
                      <span className={`text-[10px] font-bold px-1.5 py-0.5 rounded ${
                        bet.side === 'back' ? 'bg-[#72BBEF]/20 text-[#72BBEF]' : 'bg-[#FAA9BA]/20 text-[#FAA9BA]'
                      }`}>{bet.side.toUpperCase()}</span>
                      <span className="text-[11px] text-gray-400">@ {bet.price.toFixed(2)}</span>
                      <span className="text-[11px] text-gray-300 font-medium">{'\u20B9'}{bet.stake.toLocaleString('en-IN')}</span>
                      <span className="text-[10px] text-gray-600 ml-auto">
                        {new Date(bet.created_at).toLocaleTimeString('en-IN', { hour: '2-digit', minute: '2-digit' })}
                      </span>
                    </div>
                    <div className="text-[9px] text-gray-600 mt-0.5">ID: {bet.id}</div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// ========== Runner Row with Inline Bet Slip ==========
function RunnerRow({
  runner,
  prevOdds,
  onSelect,
  isSelected,
  selectedSide,
  marketType = "match_odds",
  pnl,
  allPositions,
  activeInlineBet,
  placedBetResult,
  onDeselect,
  onBetPlaced,
}: {
  runner: Runner;
  prevOdds?: { back: number; lay: number };
  onSelect: (r: Runner, side: "back" | "lay", price: number) => void;
  isSelected: boolean;
  selectedSide?: "back" | "lay";
  marketType?: string;
  pnl?: number;
  allPositions?: Record<string, number>;
  activeInlineBet?: ActiveInlineBet;
  placedBetResult?: PlacedBetResult | null;
  onDeselect?: () => void;
  onBetPlaced?: (result: PlacedBetResult, runnerName: string, side: "back" | "lay", price: number, stake: number) => void;
}) {
  const { isLoggedIn, refreshBalance } = useAuth();
  const { addToast } = useToast();

  // Inline bet slip local state
  const [inlinePrice, setInlinePrice] = useState(0);
  const [inlineStake, setInlineStake] = useState<number | "">("");
  const [confirmStep, setConfirmStep] = useState(false);
  const [placing, setPlacing] = useState(false);

  const INLINE_QUICK_STAKES = [100, 500, 1000, 5000, 10000, 25000];

  // Sync inline price when selection changes
  useEffect(() => {
    if (activeInlineBet) {
      setInlinePrice(activeInlineBet.price);
      setInlineStake("");
      setConfirmStep(false);
      setPlacing(false);
    }
  }, [activeInlineBet?.runnerId, activeInlineBet?.side]);

  // Keep inline price in sync with live odds (real-time updates)
  useEffect(() => {
    if (!isSelected || !activeInlineBet) return;
    const isFancyMarket = marketType === "fancy" || marketType === "session";
    let livePrice: number | undefined;
    if (isFancyMarket) {
      livePrice = activeInlineBet.side === "back" ? runner.yes_rate : runner.no_rate;
    } else if (activeInlineBet.side === "back") {
      livePrice = runner.back_prices?.[0]?.price ?? runner.back_price;
    } else {
      livePrice = runner.lay_prices?.[0]?.price ?? runner.lay_price;
    }
    if (livePrice && livePrice > 0) {
      setInlinePrice(livePrice);
      // Reset confirm step if odds changed — user must re-confirm
      setConfirmStep(false);
    }
  }, [runner.back_prices, runner.lay_prices, runner.yes_rate, runner.no_rate]);

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

  // Inline bet slip calculations
  const side = activeInlineBet?.side || "back";
  const isSession = activeInlineBet?.isSession || false;
  const stakeNum = typeof inlineStake === "number" ? inlineStake : 0;

  let profitOrLiability = 0;
  if (stakeNum > 0 && inlinePrice > 0) {
    if (isSession) {
      // Session: profit = stake * rate / 100
      profitOrLiability = stakeNum * inlinePrice / 100;
    } else if (side === "back") {
      profitOrLiability = stakeNum * (inlinePrice - 1);
    } else {
      // Lay liability
      profitOrLiability = stakeNum * (inlinePrice - 1);
    }
  }

  const onDecrPrice = () => {
    setInlinePrice((p) => Math.max(1.01, parseFloat((p - 0.01).toFixed(2))));
    setConfirmStep(false);
  };
  const onIncrPrice = () => {
    setInlinePrice((p) => parseFloat((p + 0.01).toFixed(2)));
    setConfirmStep(false);
  };

  const handlePlaceOrConfirm = async () => {
    if (!activeInlineBet || stakeNum <= 0) {
      addToast({ type: "error", title: "Enter a valid stake" });
      return;
    }

    if (!isLoggedIn) {
      addToast({ type: "error", title: "Please log in to place bets" });
      return;
    }

    if (!confirmStep) {
      // First click: show confirm button
      setConfirmStep(true);
      return;
    }

    // Second click: actually place the bet
    setPlacing(true);
    try {
      const result = await api.placeBet({
        market_id: activeInlineBet.marketId,
        selection_id: runner.selection_id || 0,
        side: activeInlineBet.side,
        price: inlinePrice,
        stake: stakeNum,
        client_ref: `inline_${Date.now()}`,
      });

      const status = "matched" as const;
      const betId = result.bet_id || '';
      const matchedAmt = result.matched_stake ?? stakeNum;
      refreshBalance?.();
      addToast({
        type: "success",
        title: `Bet placed: ${runner.name} @ ${inlinePrice.toFixed(2)} for \u20B9${stakeNum.toLocaleString("en-IN")}`,
        message: betId ? `Bet ID: ${betId} | Matched: \u20B9${Number(matchedAmt).toLocaleString("en-IN")}` : undefined,
      });

      onBetPlaced?.(
        { runnerId: activeInlineBet.runnerId, status, message: betId ? `Bet ID: ${betId} | Matched: \u20B9${Number(matchedAmt).toLocaleString("en-IN")}` : `Bet ${status}` },
        runner.name,
        activeInlineBet.side,
        inlinePrice,
        stakeNum
      );
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : "Failed to place bet";
      addToast({ type: "error", title: message });
      onBetPlaced?.(
        { runnerId: activeInlineBet.runnerId, status: "error", message },
        runner.name,
        activeInlineBet.side,
        inlinePrice,
        stakeNum
      );
    } finally {
      setPlacing(false);
      setConfirmStep(false);
    }
  };

  return (
    <div className="border-b border-gray-800/30">
      <div
        className={`flex items-center relative ${
          isSelected ? "bg-surface-light" : "hover:bg-surface-light/30"
        } transition-colors`}
      >
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
          /* FANCY / SESSION fallback: 2 columns -- YES (back) + NO (lay) */
          <div className="grid grid-cols-2 w-[110px] sm:w-[200px] flex-shrink-0">
            <OddsCell level={backLevels[2]} side="back" depth={0} runner={runner} onSelect={onSelect} direction={backDirection} isSelected={isSelected && selectedSide === "back"} />
            <OddsCell level={layLevels[0]} side="lay" depth={0} runner={runner} onSelect={onSelect} direction={layDirection} isSelected={isSelected && selectedSide === "lay"} />
          </div>
        ) : isBookmaker ? (
          /* BOOKMAKER: 2 columns -- Back + Lay (wider cells, no depth) */
          <div className="grid grid-cols-2 w-[110px] sm:w-[240px] flex-shrink-0">
            <OddsCell level={backLevels[2]} side="back" depth={0} runner={runner} onSelect={onSelect} direction={backDirection} isSelected={isSelected && selectedSide === "back"} />
            <OddsCell level={layLevels[0]} side="lay" depth={0} runner={runner} onSelect={onSelect} direction={layDirection} isSelected={isSelected && selectedSide === "lay"} />
          </div>
        ) : (
          /* MATCH ODDS: 6 columns -- 3 Back + 3 Lay */
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

      {/* Inline Bet Slip */}
      {isSelected && !placedBetResult && (
        <div className={`px-3 py-2 ${side === 'back' ? 'bg-[#72BBEF]/20 border-t border-[#72BBEF]/30' : 'bg-[#FAA9BA]/20 border-t border-[#FAA9BA]/30'}`}>
          <div className="flex items-center gap-2 flex-wrap">
            {/* Odds input with +/- */}
            <div className="flex items-center gap-0.5">
              <button
                onClick={onDecrPrice}
                className="w-6 h-8 bg-gray-800 rounded text-gray-400 text-sm hover:bg-gray-700 active:bg-gray-600 transition-colors"
              >
                -
              </button>
              <input
                type="number"
                step="0.01"
                value={inlinePrice}
                onChange={(e) => { setInlinePrice(parseFloat(e.target.value) || 0); setConfirmStep(false); }}
                className={`w-16 h-8 text-center text-sm font-bold rounded border bg-gray-900 text-white outline-none ${
                  side === 'back' ? 'border-[#72BBEF]/50 focus:border-[#72BBEF]' : 'border-[#FAA9BA]/50 focus:border-[#FAA9BA]'
                }`}
              />
              <button
                onClick={onIncrPrice}
                className="w-6 h-8 bg-gray-800 rounded text-gray-400 text-sm hover:bg-gray-700 active:bg-gray-600 transition-colors"
              >
                +
              </button>
            </div>

            {/* Stake input */}
            <input
              type="number"
              placeholder="Stake"
              value={inlineStake}
              onChange={(e) => { setInlineStake(e.target.value ? parseFloat(e.target.value) : ""); setConfirmStep(false); }}
              className={`w-20 h-8 px-2 text-sm rounded border bg-gray-900 text-white outline-none ${
                side === 'back' ? 'border-[#72BBEF]/50 focus:border-[#72BBEF]' : 'border-[#FAA9BA]/50 focus:border-[#FAA9BA]'
              }`}
            />

            {/* Quick stake buttons */}
            <div className="flex gap-0.5">
              {INLINE_QUICK_STAKES.map((a) => (
                <button
                  key={a}
                  onClick={() => { setInlineStake(a); setConfirmStep(false); }}
                  className="text-[9px] px-1.5 py-1 bg-gray-800 rounded text-gray-300 hover:bg-gray-700 hover:text-white active:bg-gray-600 transition-colors"
                >
                  {a >= 1000 ? `${a / 1000}K` : a}
                </button>
              ))}
            </div>

            {/* Profit / Liability display */}
            <span className="text-[11px] text-gray-400 ml-auto whitespace-nowrap">
              {side === 'back' ? 'Profit' : 'Liability'}:{' '}
              <span className={side === 'back' ? 'text-green-400 font-bold' : 'text-red-400 font-bold'}>
                {'\u20B9'}{profitOrLiability.toFixed(0)}
              </span>
            </span>

            {/* Place / Confirm button */}
            <button
              onClick={handlePlaceOrConfirm}
              disabled={placing || stakeNum <= 0}
              className={`px-3 py-1.5 rounded text-xs font-bold transition-all ${
                placing
                  ? 'bg-gray-600 text-gray-300 cursor-wait'
                  : confirmStep
                  ? 'bg-green-600 hover:bg-green-500 text-white px-4 py-2 text-sm'
                  : 'bg-lotus hover:bg-lotus/80 text-white'
              } ${stakeNum <= 0 ? 'opacity-50 cursor-not-allowed' : ''}`}
            >
              {placing ? 'Placing...' : confirmStep ? 'Confirm Bet' : 'Place Bet'}
            </button>

            {/* Cancel */}
            <button
              onClick={onDeselect}
              className="text-gray-500 hover:text-gray-300 text-xs transition-colors px-1"
              title="Cancel"
            >
              {'\u2715'}
            </button>
          </div>
        </div>
      )}

      {/* Inline Bet Result (shown briefly after placement) */}
      {isSelected && placedBetResult && (
        <div className={`px-3 py-2 border-t ${
          placedBetResult.status === 'matched'
            ? 'bg-green-500/15 border-green-500/30'
            : placedBetResult.status === 'error'
            ? 'bg-yellow-500/15 border-yellow-500/30'
            : 'bg-red-500/15 border-red-500/30'
        }`}>
          <div className="flex items-center gap-2">
            <span className={`text-xs font-bold ${
              placedBetResult.status === 'matched' ? 'text-green-400' : 'text-red-400'
            }`}>
              {placedBetResult.status === 'matched' ? 'Bet Placed' : 'Failed'}
            </span>
            <span className="text-[11px] text-gray-400">{placedBetResult.message}</span>
            <button
              onClick={onDeselect}
              className="text-gray-500 hover:text-gray-300 text-xs ml-auto transition-colors px-1"
            >
              {'\u2715'}
            </button>
          </div>
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
