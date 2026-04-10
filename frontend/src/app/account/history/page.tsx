"use client";

import { useMemo, useState, useEffect } from "react";
import { useAuth } from "@/lib/auth";
import { api } from "@/lib/api";
import Link from "next/link";
import Select from "@/components/Select";
import { SkeletonList } from "@/components/Skeleton";

type DatePreset = "" | "today" | "yesterday" | "7d" | "30d" | "custom";

export default function BettingHistoryPage() {
  const { isLoggedIn } = useAuth();
  const [bets, setBets] = useState<{ id: string; market_id: string; market_name: string; selection_name: string; side: string; price: number; stake: number; status: string; profit: number; profit_loss: number; created_at: string }[]>([]);
  const [loading, setLoading] = useState(true);
  const [sport, setSport] = useState("");
  const [status, setStatus] = useState("");

  // Date range filter state (same UX as wallet page)
  const [dateFrom, setDateFrom] = useState("");
  const [dateTo, setDateTo] = useState("");
  const [activePreset, setActivePreset] = useState<DatePreset>("");

  function applyPreset(preset: Exclude<DatePreset, "custom">) {
    setActivePreset(preset);
    const today = new Date();
    const toISO = (d: Date) => d.toISOString().slice(0, 10);
    if (preset === "") {
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
    if (isLoggedIn) loadHistory();
  }, [isLoggedIn]);

  async function loadHistory() {
    setLoading(true);
    try {
      const data = await api.request<{ bets: typeof bets; total: number } | typeof bets>("/api/v1/bets?limit=100", { auth: true });
      if (Array.isArray(data)) {
        setBets(data);
      } else if (data && typeof data === "object" && "bets" in data) {
        setBets(Array.isArray(data.bets) ? data.bets : []);
      } else {
        setBets([]);
      }
    } catch {
      // silent
    } finally {
      setLoading(false);
    }
  }

  if (!isLoggedIn) {
    return (
      <div className="max-w-7xl mx-auto px-3 py-16 text-center">
        <h2 className="text-lg font-bold text-white">Please Login</h2>
        <Link href="/login" className="inline-block mt-4 bg-lotus text-white px-6 py-2 rounded-lg text-sm">Login</Link>
      </div>
    );
  }

  const filtered = useMemo(() => {
    return bets.filter((b) => {
      if (status && b.status !== status) return false;
      if (dateFrom || dateTo) {
        const betDate = new Date(b.created_at);
        if (dateFrom) {
          const from = new Date(dateFrom);
          from.setHours(0, 0, 0, 0);
          if (betDate < from) return false;
        }
        if (dateTo) {
          const to = new Date(dateTo);
          to.setHours(23, 59, 59, 999);
          if (betDate > to) return false;
        }
      }
      return true;
    });
  }, [bets, status, dateFrom, dateTo]);

  return (
    <div className="max-w-5xl mx-auto px-3 py-4 space-y-4">
      <div className="flex items-center gap-2 text-xs text-gray-500">
        <Link href="/account" className="hover:text-white transition">Account</Link>
        <span>/</span>
        <span className="text-white">Betting History</span>
      </div>

      <h1 className="text-lg font-bold text-white">Betting History</h1>

      {/* Filters */}
      <div className="space-y-2">
        <div className="flex gap-2 flex-wrap">
          <Select value={sport} onChange={setSport} placeholder="All Sports" className="w-36"
            options={[{ value: "", label: "All Sports" }, { value: "cricket", label: "Cricket" }, { value: "football", label: "Football" }, { value: "tennis", label: "Tennis" }]}
          />
          <Select value={status} onChange={setStatus} placeholder="All Status" className="w-36"
            options={[{ value: "", label: "All Status" }, { value: "matched", label: "Matched" }, { value: "unmatched", label: "Unmatched" }, { value: "settled", label: "Settled" }, { value: "cancelled", label: "Cancelled" }]}
          />
        </div>

        {/* Date range presets */}
        <div className="flex items-center gap-1 flex-wrap">
          {([
            { key: "", label: "All" },
            { key: "today", label: "Today" },
            { key: "yesterday", label: "Yesterday" },
            { key: "7d", label: "Last 7 Days" },
            { key: "30d", label: "Last 30 Days" },
          ] as const).map((p) => (
            <button
              key={p.key || "all"}
              onClick={() => applyPreset(p.key)}
              className={`text-[10px] font-medium px-2.5 py-1 rounded-md transition ${
                activePreset === p.key
                  ? "bg-lotus text-white"
                  : "bg-surface text-gray-400 hover:text-white border border-gray-700"
              }`}
            >
              {p.label}
            </button>
          ))}
        </div>

        {/* Custom date range */}
        <div className="flex items-center gap-2 flex-wrap">
          <div className="flex items-center gap-1.5">
            <label className="text-[10px] text-gray-500">From</label>
            <input
              type="date"
              value={dateFrom}
              onChange={(e) => { setDateFrom(e.target.value); setActivePreset("custom"); }}
              className="bg-surface border border-gray-700 rounded-md px-2 py-1 text-xs text-white focus:outline-none focus:border-gray-500 [color-scheme:dark]"
            />
          </div>
          <div className="flex items-center gap-1.5">
            <label className="text-[10px] text-gray-500">To</label>
            <input
              type="date"
              value={dateTo}
              onChange={(e) => { setDateTo(e.target.value); setActivePreset("custom"); }}
              className="bg-surface border border-gray-700 rounded-md px-2 py-1 text-xs text-white focus:outline-none focus:border-gray-500 [color-scheme:dark]"
            />
          </div>
          {(dateFrom || dateTo) && (
            <button
              onClick={() => { setDateFrom(""); setDateTo(""); setActivePreset(""); }}
              className="text-[10px] text-gray-500 hover:text-white transition px-1.5 py-1 rounded"
            >
              Clear
            </button>
          )}
        </div>
      </div>

      {loading ? (
        <SkeletonList count={5} />
      ) : filtered.length === 0 ? (
        <div className="text-center py-16 text-gray-500">
          <p className="text-sm">No betting history yet</p>
          <Link href="/sports/cricket" className="text-lotus text-xs mt-2 inline-block">Place a bet</Link>
        </div>
      ) : (
        <div className="space-y-1">
          {filtered.map((bet) => {
            const pnl = bet.profit_loss ?? bet.profit ?? 0;
            return (
              <div key={bet.id} className="bg-surface rounded-lg border border-gray-800/60 p-3 flex items-center justify-between">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className={`text-[10px] px-1.5 py-0.5 rounded font-medium ${bet.side === "back" ? "bg-back/20 text-back" : "bg-lay/20 text-lay"}`}>
                      {bet.side?.toUpperCase()}
                    </span>
                    <span className="text-xs text-white truncate">{bet.market_name || bet.market_id}</span>
                  </div>
                  <div className="text-[10px] text-gray-500 mt-0.5">
                    {bet.selection_name && <span>{bet.selection_name} | </span>}
                    Odds: {bet.price} | Stake: ₹{bet.stake?.toLocaleString("en-IN")}
                  </div>
                  <div className="flex items-center gap-2 mt-0.5">
                    <span className={`text-[10px] px-1.5 py-0.5 rounded font-medium ${
                      bet.status === "matched" ? "bg-green-500/10 text-green-400" :
                      bet.status === "settled" ? "bg-blue-500/10 text-blue-400" :
                      bet.status === "cancelled" ? "bg-red-500/10 text-red-400" :
                      "bg-yellow-500/10 text-yellow-400"
                    }`}>
                      {bet.status?.toUpperCase()}
                    </span>
                    <span className="text-[10px] text-gray-600">
                      {new Date(bet.created_at).toLocaleString("en-IN", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" })}
                    </span>
                  </div>
                </div>
                <div className="text-right flex-shrink-0">
                  {bet.status === "settled" ? (
                    <div className={`text-xs font-mono font-medium ${pnl >= 0 ? "text-profit" : "text-loss"}`}>
                      {pnl >= 0 ? "+" : ""}₹{Math.abs(pnl)?.toLocaleString("en-IN")}
                    </div>
                  ) : (
                    <div className="text-[10px] text-gray-500">
                      ₹{bet.stake?.toLocaleString("en-IN")}
                    </div>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
