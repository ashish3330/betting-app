"use client";

import { useEffect, useState, useMemo } from "react";
import { api, WalletBalance, LedgerEntry } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useRouter } from "next/navigation";
import Pagination from "@/components/Pagination";
import { SkeletonPanel } from "@/components/Skeleton";

const LEDGER_PER_PAGE = 20;

const TX_TYPE_BADGES: Record<string, { label: string; className: string }> = {
  deposit: { label: "Deposit", className: "bg-green-500/20 text-green-400" },
  withdrawal: { label: "Withdrawal", className: "bg-red-500/20 text-red-400" },
  bet: { label: "Bet", className: "bg-blue-500/20 text-blue-400" },
  settlement: { label: "Settlement", className: "bg-orange-500/20 text-orange-400" },
  hold: { label: "Hold", className: "bg-yellow-500/20 text-yellow-400" },
  release: { label: "Release", className: "bg-gray-500/20 text-gray-400" },
};

function getTxBadge(type: string) {
  const key = type.toLowerCase();
  return TX_TYPE_BADGES[key] || { label: type, className: "bg-gray-500/20 text-gray-400" };
}

function formatMoney(value: number | undefined | null) {
  const n = typeof value === "number" && isFinite(value) ? value : 0;
  return (
    "\u20B9" +
    n.toLocaleString("en-IN", {
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    })
  );
}

type TxFilter = "all" | "deposits" | "withdrawals" | "bets" | "settlements";

const TX_FILTERS: { key: TxFilter; label: string }[] = [
  { key: "all", label: "All" },
  { key: "deposits", label: "Deposits" },
  { key: "withdrawals", label: "Withdrawals" },
  { key: "bets", label: "Bets" },
  { key: "settlements", label: "Settlements" },
];

function matchesTxFilter(type: string, filter: TxFilter): boolean {
  if (filter === "all") return true;
  const t = type.toLowerCase();
  if (filter === "deposits") return t === "deposit";
  if (filter === "withdrawals") return t === "withdrawal";
  if (filter === "bets") return t === "bet" || t === "hold" || t === "release";
  if (filter === "settlements") return t === "settlement";
  return true;
}

export default function WalletPage() {
  const { isLoggedIn, isLoading: authLoading } = useAuth();
  const router = useRouter();
  const [balance, setBalance] = useState<WalletBalance | null>(null);
  const [ledger, setLedger] = useState<LedgerEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [ledgerPage, setLedgerPage] = useState(1);

  // Date range filter state
  const [dateFrom, setDateFrom] = useState("");
  const [dateTo, setDateTo] = useState("");
  const [activePreset, setActivePreset] = useState<string>("");

  // Tx type filter state
  const [txFilter, setTxFilter] = useState<TxFilter>("all");

  // Apply a quick-select preset (today / yesterday / 7d / 30d / all)
  function applyPreset(preset: "today" | "yesterday" | "7d" | "30d" | "all") {
    setActivePreset(preset);
    const today = new Date();
    const toISO = (d: Date) => d.toISOString().slice(0, 10);
    if (preset === "all") {
      setDateFrom("");
      setDateTo("");
      return;
    }
    if (preset === "today") {
      const iso = toISO(today);
      setDateFrom(iso);
      setDateTo(iso);
      return;
    }
    if (preset === "yesterday") {
      const y = new Date(today);
      y.setDate(today.getDate() - 1);
      const iso = toISO(y);
      setDateFrom(iso);
      setDateTo(iso);
      return;
    }
    if (preset === "7d") {
      const from = new Date(today);
      from.setDate(today.getDate() - 6);
      setDateFrom(toISO(from));
      setDateTo(toISO(today));
      return;
    }
    if (preset === "30d") {
      const from = new Date(today);
      from.setDate(today.getDate() - 29);
      setDateFrom(toISO(from));
      setDateTo(toISO(today));
      return;
    }
  }

  useEffect(() => {
    if (!authLoading && !isLoggedIn) {
      router.push("/login");
      return;
    }
    if (isLoggedIn) {
      loadData();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isLoggedIn, authLoading, router]);

  async function loadData() {
    try {
      const [bal, led] = await Promise.all([
        api.getBalance(),
        api.getLedger().catch(() => []),
      ]);
      setBalance(bal);
      setLedger(Array.isArray(led) ? led : []);
    } catch {
      // API not available
    } finally {
      setLoading(false);
    }
  }

  // Pending withdrawals (from ledger description or type heuristic)
  const pendingWithdrawals = useMemo(() => {
    // Any withdrawal-type entry whose description includes "pending" (case-insensitive)
    // or whose amount is negative but settlement is not yet complete.
    const items = ledger.filter((e) => {
      const t = e.type?.toLowerCase() || "";
      const desc = (e.description || "").toLowerCase();
      return t === "withdrawal" && (desc.includes("pending") || desc.includes("processing") || desc.includes("requested"));
    });
    const total = items.reduce((sum, e) => sum + Math.abs(e.amount), 0);
    return { count: items.length, total };
  }, [ledger]);

  // Filtered ledger based on date range + tx type filter
  const filteredLedger = useMemo(() => {
    return ledger.filter((entry) => {
      if (!matchesTxFilter(entry.type, txFilter)) return false;
      const entryDate = new Date(entry.created_at);
      if (dateFrom) {
        const from = new Date(dateFrom);
        from.setHours(0, 0, 0, 0);
        if (entryDate < from) return false;
      }
      if (dateTo) {
        const to = new Date(dateTo);
        to.setHours(23, 59, 59, 999);
        if (entryDate > to) return false;
      }
      return true;
    });
  }, [ledger, dateFrom, dateTo, txFilter]);

  // Reset page when filters change
  useEffect(() => {
    setLedgerPage(1);
  }, [dateFrom, dateTo, txFilter]);

  if (authLoading || loading) {
    return (
      <div className="max-w-5xl mx-auto px-3 py-4">
        <SkeletonPanel />
      </div>
    );
  }

  const totalBalance = balance?.balance ?? 0;
  const exposure = balance?.exposure ?? 0;
  const available = balance?.available_balance ?? 0;

  return (
    <div className="max-w-5xl mx-auto px-3 py-4 space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-bold text-white">Wallet</h1>
      </div>

      {/* Hero + side stat cards */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-3">
        {/* Hero Balance Card — spans 2/3 on desktop */}
        <div className="lg:col-span-2 relative overflow-hidden rounded-2xl border border-lotus/30 bg-gradient-to-br from-lotus/20 via-surface to-surface p-5 sm:p-6">
          <div className="absolute -top-16 -right-16 w-48 h-48 bg-lotus/20 rounded-full blur-3xl pointer-events-none" />
          <div className="relative">
            <div className="text-[11px] uppercase tracking-widest text-lotus/80 font-semibold">
              Total Balance
            </div>
            <div className="mt-2 text-3xl sm:text-4xl font-bold text-white font-mono tabular-nums tracking-tight">
              {formatMoney(totalBalance)}
            </div>
            <div className="mt-1 text-[11px] text-gray-400">
              Last updated {new Date().toLocaleTimeString("en-IN", { hour: "2-digit", minute: "2-digit" })}
            </div>
            <div className="mt-5 flex flex-col sm:flex-row gap-2">
              <button
                onClick={() => router.push("/wallet/deposit")}
                className="flex-1 h-11 px-5 bg-lotus hover:bg-lotus-light text-white rounded-lg text-sm font-bold transition shadow-lg shadow-lotus/20 flex items-center justify-center gap-2"
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
                </svg>
                Deposit
              </button>
              <button
                onClick={() => router.push("/wallet/withdraw")}
                className="flex-1 h-11 px-5 bg-surface-lighter hover:bg-surface-light text-white border border-gray-700 hover:border-gray-500 rounded-lg text-sm font-bold transition flex items-center justify-center gap-2"
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M20 12H4" />
                </svg>
                Withdraw
              </button>
            </div>
          </div>
        </div>

        {/* Side stat cards */}
        <div className="grid grid-cols-2 lg:grid-cols-1 gap-3">
          <div
            className={`rounded-2xl border p-4 ${
              exposure > 0
                ? "border-loss/30 bg-loss/5"
                : "border-gray-800 bg-surface"
            }`}
          >
            <div className="text-[10px] uppercase tracking-wider text-gray-500 font-semibold">
              Exposure
            </div>
            <div
              className={`mt-1 text-xl sm:text-2xl font-bold font-mono tabular-nums ${
                exposure > 0 ? "text-loss" : "text-gray-300"
              }`}
            >
              {exposure > 0 ? "- " : ""}
              {formatMoney(Math.abs(exposure))}
            </div>
            <div className="text-[10px] text-gray-500 mt-0.5">Held in open bets</div>
          </div>

          <div className="rounded-2xl border border-profit/30 bg-profit/5 p-4">
            <div className="text-[10px] uppercase tracking-wider text-profit/80 font-semibold">
              Available
            </div>
            <div className="mt-1 text-xl sm:text-2xl font-bold font-mono tabular-nums text-profit">
              {formatMoney(available)}
            </div>
            <div className="text-[10px] text-gray-500 mt-0.5">Ready to bet or withdraw</div>
          </div>
        </div>
      </div>

      {/* Pending withdrawals widget */}
      {pendingWithdrawals.count > 0 && (
        <div className="rounded-xl border border-yellow-500/30 bg-yellow-500/5 p-4 flex items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="w-9 h-9 rounded-full bg-yellow-500/15 text-yellow-400 flex items-center justify-center">
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            </div>
            <div>
              <div className="text-sm text-white font-semibold">
                {pendingWithdrawals.count} pending withdrawal{pendingWithdrawals.count !== 1 ? "s" : ""}
              </div>
              <div className="text-[11px] text-gray-400">
                Total {formatMoney(pendingWithdrawals.total)} awaiting processing
              </div>
            </div>
          </div>
          <button
            onClick={() => setTxFilter("withdrawals")}
            className="text-[11px] text-yellow-400 hover:text-yellow-300 font-semibold underline"
          >
            View
          </button>
        </div>
      )}

      {/* Ledger */}
      <div className="bg-surface rounded-xl border border-gray-800 overflow-hidden">
        <div className="px-3 py-2 border-b border-gray-800 space-y-2">
          <h2 className="text-xs font-medium text-gray-400 uppercase tracking-wide">
            Transaction History
          </h2>

          {/* Transaction type tabs */}
          <div className="flex items-center gap-1 overflow-x-auto no-scrollbar">
            {TX_FILTERS.map((f) => (
              <button
                key={f.key}
                onClick={() => setTxFilter(f.key)}
                className={`text-[11px] font-semibold px-3 py-1.5 rounded-md whitespace-nowrap transition ${
                  txFilter === f.key
                    ? "bg-lotus text-white"
                    : "bg-surface-lighter text-gray-400 hover:text-white border border-gray-700"
                }`}
              >
                {f.label}
              </button>
            ))}
          </div>

          {/* Date Range Filter — preset buttons + custom range */}
          <div className="space-y-2">
            <div className="flex items-center gap-1 flex-wrap">
              {([
                { key: "today", label: "Today" },
                { key: "yesterday", label: "Yesterday" },
                { key: "7d", label: "Last 7 Days" },
                { key: "30d", label: "Last 30 Days" },
                { key: "all", label: "All" },
              ] as const).map((p) => (
                <button
                  key={p.key}
                  onClick={() => applyPreset(p.key)}
                  className={`text-[10px] font-medium px-2.5 py-1 rounded-md transition ${
                    activePreset === p.key
                      ? "bg-lotus text-white"
                      : "bg-surface-lighter text-gray-400 hover:text-white border border-gray-700"
                  }`}
                >
                  {p.label}
                </button>
              ))}
            </div>
            <div className="flex items-center gap-2 flex-wrap">
              <div className="flex items-center gap-1.5">
                <label className="text-[10px] text-gray-500">From</label>
                <input
                  type="date"
                  value={dateFrom}
                  onChange={(e) => {
                    setDateFrom(e.target.value);
                    setActivePreset("");
                  }}
                  className="bg-surface-lighter border border-gray-700 rounded-md px-2 py-1 text-xs text-white focus:outline-none focus:border-gray-500 [color-scheme:dark]"
                />
              </div>
              <div className="flex items-center gap-1.5">
                <label className="text-[10px] text-gray-500">To</label>
                <input
                  type="date"
                  value={dateTo}
                  onChange={(e) => {
                    setDateTo(e.target.value);
                    setActivePreset("");
                  }}
                  className="bg-surface-lighter border border-gray-700 rounded-md px-2 py-1 text-xs text-white focus:outline-none focus:border-gray-500 [color-scheme:dark]"
                />
              </div>
              {(dateFrom || dateTo) && (
                <button
                  onClick={() => {
                    setDateFrom("");
                    setDateTo("");
                    setActivePreset("");
                  }}
                  className="text-[10px] text-gray-500 hover:text-white transition px-1.5 py-1 rounded"
                >
                  Clear
                </button>
              )}
            </div>
          </div>
        </div>

        {filteredLedger.length > 0 ? (
          <>
            <div className="divide-y divide-gray-800/50">
              {filteredLedger
                .slice((ledgerPage - 1) * LEDGER_PER_PAGE, ledgerPage * LEDGER_PER_PAGE)
                .map((entry) => {
                  const txBadge = getTxBadge(entry.type);
                  return (
                    <div
                      key={entry.id}
                      className="px-3 py-2.5 flex items-center justify-between"
                    >
                      <div>
                        <div className="flex items-center gap-2 mb-0.5">
                          <span
                            className={`text-[10px] px-1.5 py-0.5 rounded font-medium ${txBadge.className}`}
                          >
                            {txBadge.label}
                          </span>
                        </div>
                        <div className="text-sm text-white">{entry.description}</div>
                        <div className="text-[10px] text-gray-500 mt-0.5">
                          {new Date(entry.created_at).toLocaleString("en-IN", {
                            day: "numeric",
                            month: "short",
                            hour: "2-digit",
                            minute: "2-digit",
                          })}
                        </div>
                      </div>
                      <div className="text-right">
                        <div
                          className={`text-sm font-mono tabular-nums font-bold ${
                            entry.amount >= 0 ? "text-profit" : "text-loss"
                          }`}
                        >
                          {entry.amount >= 0 ? "+" : ""}
                          {formatMoney(Math.abs(entry.amount))}
                        </div>
                        <div className="text-[10px] text-gray-500 font-mono tabular-nums">
                          Bal: {formatMoney(entry.balance_after)}
                        </div>
                      </div>
                    </div>
                  );
                })}
            </div>
            <div className="px-3 pb-3">
              <Pagination
                currentPage={ledgerPage}
                totalPages={Math.ceil(filteredLedger.length / LEDGER_PER_PAGE)}
                onPageChange={setLedgerPage}
              />
            </div>
          </>
        ) : (
          <div className="text-center py-12 text-gray-500 text-sm">
            {dateFrom || dateTo || txFilter !== "all"
              ? "No transactions match these filters"
              : "No transactions yet"}
          </div>
        )}
      </div>
    </div>
  );
}
