"use client";

import { useEffect, useState, useCallback, useMemo } from "react";
import { useAuth } from "@/lib/auth";
import { useRouter } from "next/navigation";
import { useToast } from "@/components/Toast";
import { api } from "@/lib/api";
import Pagination from "@/components/Pagination";

interface Bet {
  id: string;
  market_id: string;
  market_name?: string;
  selection_name?: string;
  market_type?: string;
  sport?: string;
  display_side?: string;
  side: "back" | "lay";
  price: number;
  stake: number;
  status: string;
  matched_stake?: number;
  profit_loss?: number;
  created_at: string;
}

const BETS_PER_PAGE = 10;

type SportFilter = "all" | "cricket" | "football" | "tennis";
type DateFilter = "all" | "today" | "7d" | "30d";

function truncateId(id: string): string {
  return id.length > 8 ? id.slice(-8) : id;
}

function getStatusBadge(status: string, profitLoss?: number) {
  const s = status.toLowerCase();
  if (s === "matched" || s === "open" || s === "partially_matched") {
    return { label: "Matched", className: "bg-profit/20 text-profit" };
  }
  if (s === "settled" || s === "won") {
    if (profitLoss !== undefined && profitLoss !== null) {
      return profitLoss >= 0
        ? { label: "Settled (Won)", className: "bg-profit/20 text-profit" }
        : { label: "Settled (Lost)", className: "bg-loss/20 text-loss" };
    }
    return { label: "Settled (Won)", className: "bg-profit/20 text-profit" };
  }
  if (s === "lost") {
    return { label: "Settled (Lost)", className: "bg-loss/20 text-loss" };
  }
  if (s === "void" || s === "voided" || s === "cancelled") {
    return { label: "Void", className: "bg-gray-500/20 text-gray-400" };
  }
  return { label: status.toUpperCase(), className: "bg-amber-500/20 text-amber-400" };
}

function calcPotentialProfit(bet: Bet): number {
  const isFancy = bet.market_type === "fancy" || bet.market_type === "session";
  const ds = bet.display_side || bet.side;
  if (isFancy) {
    return ds === "yes" || bet.side === "back" ? bet.stake * bet.price / 100 : bet.stake;
  }
  if (bet.side === "back") return bet.stake * (bet.price - 1);
  return bet.stake;
}

function calcLiability(bet: Bet): number | null {
  const isFancy = bet.market_type === "fancy" || bet.market_type === "session";
  const ds = bet.display_side || bet.side;
  if (isFancy) {
    return ds === "no" || bet.side === "lay" ? bet.stake * bet.price / 100 : null;
  }
  if (bet.side !== "lay") return null;
  return bet.stake * (bet.price - 1);
}

/** Promise.all with a max concurrency cap, preserves input order. */
async function pMapLimit<T, R>(items: T[], limit: number, fn: (item: T) => Promise<R>): Promise<R[]> {
  const results: R[] = new Array(items.length);
  let idx = 0;
  const workers = Array.from({ length: Math.min(limit, items.length) }, async () => {
    while (true) {
      const current = idx++;
      if (current >= items.length) return;
      results[current] = await fn(items[current]);
    }
  });
  await Promise.all(workers);
  return results;
}

export default function BetsPage() {
  const { isLoggedIn, isLoading: authLoading } = useAuth();
  const router = useRouter();
  const { addToast } = useToast();
  const [bets, setBets] = useState<Bet[]>([]);
  const [totalBets, setTotalBets] = useState(0);
  const [loading, setLoading] = useState(true);
  const [tab, setTab] = useState<"open" | "settled" | "all">("open");
  const [currentPage, setCurrentPage] = useState(1);

  // Filters
  const [sportFilter, setSportFilter] = useState<SportFilter>("all");
  const [dateFilter, setDateFilter] = useState<DateFilter>("all");

  // Cashout state
  const [cashoutOffers, setCashoutOffers] = useState<Record<string, number>>({});
  const [cashoutLoading, setCashoutLoading] = useState<Record<string, boolean>>({});
  const [confirmCashout, setConfirmCashout] = useState<string | null>(null);

  useEffect(() => {
    if (!authLoading && !isLoggedIn) {
      router.push("/login");
      return;
    }
    if (isLoggedIn) {
      loadBets();
    }
  }, [isLoggedIn, authLoading, router, tab, currentPage]);

  useEffect(() => {
    setCurrentPage(1);
  }, [tab, sportFilter, dateFilter]);

  async function loadBets() {
    try {
      const statusMap: Record<string, string> = { open: "open", settled: "settled" };
      const statusParam = statusMap[tab] ? `&status=${statusMap[tab]}` : "";
      const data = await api.request<{ bets: Bet[]; total: number; page: number; limit: number } | Bet[]>(
        `/api/v1/bets?page=${currentPage}&limit=${BETS_PER_PAGE}${statusParam}`, { auth: true }
      );
      let betList: Bet[] = [];
      if (Array.isArray(data)) {
        betList = data;
        setBets(data);
        setTotalBets(data.length);
      } else {
        betList = data.bets || [];
        setBets(betList);
        setTotalBets(data.total || 0);
      }
      fetchCashoutOffers(betList);
    } catch {
      // API might not have this endpoint
    } finally {
      setLoading(false);
    }
  }

  // Bulk-fetch cashout offers for open bets, capped at 5 concurrent requests.
  const fetchCashoutOffers = useCallback(async (betList: Bet[]) => {
    const matchedBets = betList.filter(
      (b) => b.status === "matched" || b.status === "open" || b.status === "partially_matched"
    );
    if (matchedBets.length === 0) {
      setCashoutOffers({});
      return;
    }
    const results = await pMapLimit(matchedBets, 5, (bet) =>
      api
        .getCashoutOffer(bet.id)
        .then((data) => ({ id: bet.id, offer: data.offer }))
        .catch(() => ({ id: bet.id, offer: 0 }))
    );
    const offers: Record<string, number> = {};
    for (const { id, offer } of results) {
      if (offer && offer > 0) offers[id] = offer;
    }
    setCashoutOffers(offers);
  }, []);

  async function handleCashout(betId: string) {
    setCashoutLoading((prev) => ({ ...prev, [betId]: true }));
    try {
      const result = await api.acceptCashout(betId);
      addToast({
        type: "success",
        title: "Cash Out Successful",
        message: result.message || `Cashed out for \u20B9${cashoutOffers[betId]?.toLocaleString("en-IN", { minimumFractionDigits: 2 })}`,
      });
      loadBets();
    } catch (err) {
      addToast({
        type: "error",
        title: "Cash Out Failed",
        message: err instanceof Error ? err.message : "Something went wrong",
      });
    } finally {
      setCashoutLoading((prev) => ({ ...prev, [betId]: false }));
      setConfirmCashout(null);
    }
  }

  // Apply client-side filters on top of the paginated server response.
  const filteredBets = useMemo(() => {
    const now = Date.now();
    const day = 24 * 60 * 60 * 1000;
    return bets.filter((b) => {
      if (sportFilter !== "all") {
        const sport = (b.sport || "").toLowerCase();
        if (!sport.includes(sportFilter)) return false;
      }
      if (dateFilter !== "all") {
        const t = new Date(b.created_at).getTime();
        if (dateFilter === "today") {
          const start = new Date();
          start.setHours(0, 0, 0, 0);
          if (t < start.getTime()) return false;
        } else if (dateFilter === "7d") {
          if (now - t > 7 * day) return false;
        } else if (dateFilter === "30d") {
          if (now - t > 30 * day) return false;
        }
      }
      return true;
    });
  }, [bets, sportFilter, dateFilter]);

  // Summary strip (calculated from the filtered view)
  const summary = useMemo(() => {
    const totalStake = filteredBets.reduce((sum, b) => sum + (b.stake || 0), 0);
    const totalPnL = filteredBets.reduce((sum, b) => sum + (b.profit_loss ?? 0), 0);
    const settled = filteredBets.filter((b) => b.profit_loss !== undefined && b.profit_loss !== null);
    const won = settled.filter((b) => (b.profit_loss ?? 0) > 0).length;
    const winRate = settled.length > 0 ? (won / settled.length) * 100 : 0;
    return {
      count: filteredBets.length,
      totalStake,
      totalPnL,
      winRate,
      hasSettled: settled.length > 0,
    };
  }, [filteredBets]);

  const paginatedBets = filteredBets;
  const totalPages = Math.ceil(totalBets / BETS_PER_PAGE);

  if (authLoading) return null;

  return (
    <div className="max-w-3xl mx-auto px-3 py-4 space-y-4">
      <h1 className="text-lg font-bold text-white">My Bets</h1>

      {/* Tabs */}
      <div className="flex gap-1 bg-surface rounded-lg p-0.5 w-fit">
        {(["open", "settled", "all"] as const).map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`px-4 py-1.5 rounded-md text-xs font-medium transition capitalize ${
              tab === t
                ? "bg-surface-lighter text-white"
                : "text-gray-500 hover:text-gray-300"
            }`}
          >
            {t}
          </button>
        ))}
      </div>

      {/* Filter row */}
      <div className="flex flex-wrap items-center gap-2">
        <label className="text-[10px] text-gray-500 uppercase tracking-wider mr-1">Sport</label>
        <select
          value={sportFilter}
          onChange={(e) => setSportFilter(e.target.value as SportFilter)}
          className="bg-surface border border-gray-800 text-xs text-white rounded-md px-2 py-1.5 focus:outline-none focus:border-gray-600"
        >
          <option value="all">All sports</option>
          <option value="cricket">Cricket</option>
          <option value="football">Football</option>
          <option value="tennis">Tennis</option>
        </select>

        <label className="text-[10px] text-gray-500 uppercase tracking-wider mr-1 ml-2">Date</label>
        <select
          value={dateFilter}
          onChange={(e) => setDateFilter(e.target.value as DateFilter)}
          className="bg-surface border border-gray-800 text-xs text-white rounded-md px-2 py-1.5 focus:outline-none focus:border-gray-600"
        >
          <option value="all">All time</option>
          <option value="today">Today</option>
          <option value="7d">Last 7 days</option>
          <option value="30d">Last 30 days</option>
        </select>

        {(sportFilter !== "all" || dateFilter !== "all") && (
          <button
            onClick={() => {
              setSportFilter("all");
              setDateFilter("all");
            }}
            className="text-[11px] text-gray-500 hover:text-white transition ml-1"
          >
            Clear
          </button>
        )}
      </div>

      {/* P&L Summary Strip */}
      {!loading && filteredBets.length > 0 && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
          <div className="bg-surface border border-gray-800 rounded-lg p-2.5">
            <div className="text-[10px] text-gray-500 uppercase tracking-wider">Bets</div>
            <div className="text-sm font-mono text-white mt-0.5">{summary.count}</div>
          </div>
          <div className="bg-surface border border-gray-800 rounded-lg p-2.5">
            <div className="text-[10px] text-gray-500 uppercase tracking-wider">Total Stake</div>
            <div className="text-sm font-mono text-white mt-0.5">
              {"\u20B9"}
              {summary.totalStake.toLocaleString("en-IN", { minimumFractionDigits: 2 })}
            </div>
          </div>
          <div className="bg-surface border border-gray-800 rounded-lg p-2.5">
            <div className="text-[10px] text-gray-500 uppercase tracking-wider">P&amp;L</div>
            <div
              className={`text-sm font-mono mt-0.5 font-semibold ${
                summary.totalPnL >= 0 ? "text-profit" : "text-loss"
              }`}
            >
              {summary.totalPnL >= 0 ? "+" : "-"}
              {"\u20B9"}
              {Math.abs(summary.totalPnL).toLocaleString("en-IN", { minimumFractionDigits: 2 })}
            </div>
          </div>
          <div className="bg-surface border border-gray-800 rounded-lg p-2.5">
            <div className="text-[10px] text-gray-500 uppercase tracking-wider">Win Rate</div>
            <div className="text-sm font-mono text-white mt-0.5">
              {summary.hasSettled ? `${summary.winRate.toFixed(0)}%` : "-"}
            </div>
          </div>
        </div>
      )}

      {/* Bets List */}
      {loading ? (
        <div className="space-y-3">
          {Array.from({ length: 5 }).map((_, i) => (
            <div
              key={i}
              className="bg-surface rounded-xl border border-gray-800 h-24 animate-pulse"
            />
          ))}
        </div>
      ) : paginatedBets.length > 0 ? (
        <>
          {(() => {
            const matchBets = paginatedBets.filter(
              (b) => b.market_type !== "fancy" && b.market_type !== "session"
            );
            const fancyBets = paginatedBets.filter(
              (b) => b.market_type === "fancy" || b.market_type === "session"
            );

            const renderBetCard = (bet: Bet) => {
              const statusBadge = getStatusBadge(bet.status, bet.profit_loss);
              const potentialProfit = calcPotentialProfit(bet);
              const liability = calcLiability(bet);

              return (
                <div
                  key={bet.id}
                  className="bg-surface rounded-xl border border-gray-800 p-3"
                >
                  <div className="flex items-start justify-between">
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 mb-1 flex-wrap">
                        {(bet.market_type === "fancy" || bet.market_type === "session") && (
                          <span className="text-[8px] font-bold px-1 py-0.5 rounded bg-purple-500/20 text-purple-400">FANCY</span>
                        )}
                        <span
                          className={`text-[10px] font-bold px-1.5 py-0.5 rounded ${
                            (bet.display_side || bet.side) === "back" || (bet.display_side || bet.side) === "yes"
                              ? "bg-back/20 text-back"
                              : "bg-lay/20 text-lay"
                          }`}
                        >
                          {(bet.display_side || bet.side).toUpperCase()}
                        </span>
                        <span
                          className={`text-[10px] px-1.5 py-0.5 rounded ${statusBadge.className}`}
                        >
                          {statusBadge.label}
                        </span>
                        <span className="text-[9px] text-gray-600 font-mono">
                          #{truncateId(bet.id)}
                        </span>
                      </div>
                      <div className="text-sm text-white font-medium truncate">
                        {bet.selection_name || bet.market_name || bet.market_id}
                      </div>
                      <div className="text-[10px] text-gray-500 mt-0.5">
                        {new Date(bet.created_at).toLocaleString("en-IN", {
                          day: "numeric",
                          month: "short",
                          hour: "2-digit",
                          minute: "2-digit",
                        })}
                      </div>
                    </div>
                    <div className="text-right ml-3 space-y-0.5">
                      <div className="text-xs text-gray-400">
                        Odds:{" "}
                        <span className="font-mono text-white">
                          {bet.price.toFixed(2)}
                        </span>
                      </div>
                      <div className="text-xs text-gray-400">
                        Stake:{" "}
                        <span className="font-mono text-white">
                          {"\u20B9"}
                          {bet.stake.toLocaleString("en-IN")}
                        </span>
                      </div>
                      <div className="text-xs text-gray-400">
                        Pot. Profit:{" "}
                        <span className="font-mono text-profit">
                          {"\u20B9"}
                          {potentialProfit.toLocaleString("en-IN", {
                            minimumFractionDigits: 2,
                          })}
                        </span>
                      </div>
                      {liability !== null && (
                        <div className="text-xs text-gray-400">
                          Liability:{" "}
                          <span className="font-mono text-loss">
                            {"\u20B9"}
                            {liability.toLocaleString("en-IN", {
                              minimumFractionDigits: 2,
                            })}
                          </span>
                        </div>
                      )}
                      {bet.profit_loss !== undefined &&
                        bet.profit_loss !== null && (
                          <div
                            className={`text-sm font-bold font-mono mt-1 ${
                              bet.profit_loss >= 0 ? "text-profit" : "text-loss"
                            }`}
                          >
                            P&L: {bet.profit_loss >= 0 ? "+" : ""}
                            {"\u20B9"}
                            {Math.abs(bet.profit_loss).toLocaleString("en-IN", {
                              minimumFractionDigits: 2,
                            })}
                          </div>
                        )}
                    </div>
                  </div>

                  {/* Cash Out Button */}
                  {cashoutOffers[bet.id] &&
                    (bet.status === "matched" ||
                      bet.status === "open" ||
                      bet.status === "partially_matched") && (
                      <div className="mt-2 pt-2 border-t border-gray-800/50">
                        {confirmCashout === bet.id ? (
                          <div className="flex items-center gap-2">
                            <span className="text-xs text-gray-400">
                              Confirm cash out for{" "}
                              <span className="text-profit font-bold">
                                {"\u20B9"}
                                {cashoutOffers[bet.id].toLocaleString("en-IN", {
                                  minimumFractionDigits: 2,
                                })}
                              </span>
                              ?
                            </span>
                            <button
                              onClick={() => handleCashout(bet.id)}
                              disabled={cashoutLoading[bet.id]}
                              className="text-[11px] bg-profit/20 text-profit hover:bg-profit/30 px-3 py-1 rounded-md font-medium transition disabled:opacity-50"
                            >
                              {cashoutLoading[bet.id] ? "Processing..." : "Yes"}
                            </button>
                            <button
                              onClick={() => setConfirmCashout(null)}
                              className="text-[11px] text-gray-500 hover:text-white px-2 py-1 rounded-md transition"
                            >
                              No
                            </button>
                          </div>
                        ) : (
                          <button
                            onClick={() => setConfirmCashout(bet.id)}
                            className="w-full text-xs bg-profit/10 text-profit hover:bg-profit/20 border border-profit/30 px-3 py-1.5 rounded-lg font-bold transition"
                          >
                            Cash Out {"\u20B9"}
                            {cashoutOffers[bet.id].toLocaleString("en-IN", {
                              minimumFractionDigits: 2,
                            })}
                          </button>
                        )}
                      </div>
                    )}
                </div>
              );
            };

            return (
              <div className="space-y-4">
                {matchBets.length > 0 && (
                  <div className="space-y-2">
                    <div className="flex items-center gap-2">
                      <h2 className="text-sm font-semibold text-gray-300">Match Bets</h2>
                      <span className="text-[10px] font-bold bg-white/10 text-gray-400 px-1.5 py-0.5 rounded-full">{matchBets.length}</span>
                    </div>
                    {matchBets.map(renderBetCard)}
                  </div>
                )}
                {fancyBets.length > 0 && (
                  <div className="space-y-2">
                    <div className="flex items-center gap-2">
                      <h2 className="text-sm font-semibold text-purple-400">Fancy Bets</h2>
                      <span className="text-[10px] font-bold bg-purple-500/20 text-purple-400 px-1.5 py-0.5 rounded-full">{fancyBets.length}</span>
                    </div>
                    {fancyBets.map(renderBetCard)}
                  </div>
                )}
              </div>
            );
          })()}

          <Pagination
            currentPage={currentPage}
            totalPages={totalPages}
            onPageChange={setCurrentPage}
          />
        </>
      ) : (
        <div className="text-center py-16">
          <svg
            className="w-12 h-12 mx-auto text-gray-500 mb-3"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={1}
              d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2"
            />
          </svg>
          <h3 className="text-base font-medium text-gray-400">
            No {tab !== "all" ? tab : ""} bets
          </h3>
          <p className="text-xs text-gray-400 mt-1">
            {tab === "open"
              ? "Place a bet from the markets page"
              : "Your settled bets will appear here"}
          </p>
        </div>
      )}
    </div>
  );
}
