"use client";

import { useEffect, useState } from "react";
import { useAuth } from "@/lib/auth";
import { api } from "@/lib/api";
import { decryptLocalStorage } from "@/lib/crypto";
import Pagination from "@/components/Pagination";

const SETTLEMENTS_PER_PAGE = 20;

// ── Types ───────────────────────────────────────────────────────────────────

interface PnLEntry {
  date: string;
  bets: number;
  stake: number;
  pnl: number;
}

interface VolumeEntry {
  sport: string;
  bets: number;
  volume: number;
}

interface SettlementEntry {
  bet_id: string;
  user: string;
  market: string;
  side: string;
  stake: number;
  pnl: number;
  settled_at: string;
}

// ── Chart Components ────────────────────────────────────────────────────────

function BarChart({ data }: { data: { label: string; value: number }[] }) {
  const max = Math.max(...data.map((d) => Math.abs(d.value)), 1);
  return (
    <div className="flex items-end gap-1 h-40">
      {data.map((d, i) => (
        <div key={i} className="flex-1 flex flex-col items-center justify-end h-full">
          <span className="text-[9px] text-gray-400 mb-1">
            {d.value >= 0 ? "+" : ""}
            {d.value.toLocaleString("en-IN", { maximumFractionDigits: 0 })}
          </span>
          <div
            className={`w-full rounded-t transition-all ${
              d.value >= 0 ? "bg-profit" : "bg-loss"
            }`}
            style={{
              height: `${Math.max((Math.abs(d.value) / max) * 100, 4)}%`,
              minHeight: "4px",
            }}
          />
          <span className="text-[9px] text-gray-500 mt-1 truncate w-full text-center">
            {d.label}
          </span>
        </div>
      ))}
    </div>
  );
}

const SPORT_COLORS = [
  "bg-blue-500",
  "bg-amber-500",
  "bg-emerald-500",
  "bg-purple-500",
  "bg-rose-500",
  "bg-cyan-500",
  "bg-orange-500",
  "bg-indigo-500",
];

function VolumeChart({ data }: { data: VolumeEntry[] }) {
  const total = data.reduce((s, d) => s + d.volume, 0) || 1;
  return (
    <div className="space-y-2">
      {data.map((d, i) => {
        const pct = (d.volume / total) * 100;
        return (
          <div key={d.sport} className="space-y-1">
            <div className="flex justify-between text-xs">
              <span className="text-gray-300 capitalize">{d.sport}</span>
              <span className="text-gray-500">
                {d.bets} bets | {pct.toFixed(1)}%
              </span>
            </div>
            <div className="h-2 bg-gray-800 rounded-full overflow-hidden">
              <div
                className={`h-full rounded-full ${SPORT_COLORS[i % SPORT_COLORS.length]}`}
                style={{ width: `${pct}%` }}
              />
            </div>
          </div>
        );
      })}
    </div>
  );
}

function StatCard({
  label,
  value,
  color = "text-white",
}: {
  label: string;
  value: string;
  color?: string;
}) {
  return (
    <div className="bg-surface border border-gray-800 rounded-lg p-4">
      <div className="text-[10px] uppercase tracking-wider text-gray-500 mb-1">
        {label}
      </div>
      <div className={`text-lg font-bold font-mono ${color}`}>{value}</div>
    </div>
  );
}

// ── CSV Download ────────────────────────────────────────────────────────────

async function downloadCSV() {
  const token = decryptLocalStorage("access_token");
  const baseUrl = process.env.NEXT_PUBLIC_API_URL || "";
  const res = await fetch(`${baseUrl}/api/v1/panel/reports/csv`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) throw new Error("Failed to download CSV");
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = "bets_report.csv";
  a.click();
  URL.revokeObjectURL(url);
}

// ── Page ────────────────────────────────────────────────────────────────────

export default function ReportsPage() {
  const { user } = useAuth();
  const [pnlData, setPnlData] = useState<PnLEntry[]>([]);
  const [volumeData, setVolumeData] = useState<VolumeEntry[]>([]);
  const [settlements, setSettlements] = useState<SettlementEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [csvLoading, setCsvLoading] = useState(false);
  const [settlementPage, setSettlementPage] = useState(1);

  useEffect(() => {
    Promise.all([
      api.request<PnLEntry[]>("/api/v1/panel/reports/pnl", { auth: true }),
      api.request<VolumeEntry[]>("/api/v1/panel/reports/volume", { auth: true }),
      api.request<SettlementEntry[]>("/api/v1/panel/reports/settlement", { auth: true }),
    ])
      .then(([pnl, vol, settle]) => {
        setPnlData(pnl);
        setVolumeData(vol);
        setSettlements(settle);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div className="space-y-4 max-w-5xl">
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="h-32 bg-surface rounded-lg animate-pulse" />
        ))}
      </div>
    );
  }

  // Compute summary stats
  const totalPnL = pnlData.reduce((s, d) => s + d.pnl, 0);
  const totalVolume = pnlData.reduce((s, d) => s + d.stake, 0);
  const totalBets = pnlData.reduce((s, d) => s + d.bets, 0);

  const wonBets = settlements.filter((s) => s.pnl > 0).length;
  const lostBets = settlements.filter((s) => s.pnl < 0).length;
  const settledTotal = wonBets + lostBets;
  const winRate = settledTotal > 0 ? ((wonBets / settledTotal) * 100).toFixed(1) : "0.0";

  const barData = pnlData.map((d) => ({
    label: d.date.slice(5), // "04-05"
    value: d.pnl,
  }));

  const handleCSV = async () => {
    setCsvLoading(true);
    try {
      await downloadCSV();
    } catch {
      // silently fail
    } finally {
      setCsvLoading(false);
    }
  };

  return (
    <div className="space-y-6 max-w-5xl">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-bold text-white">Reports</h1>
          <p className="text-xs text-gray-500 mt-0.5">
            Analytics and reporting for {user?.role === "superadmin" ? "platform" : "your downline"}
          </p>
        </div>
        <button
          onClick={handleCSV}
          disabled={csvLoading}
          className="flex items-center gap-2 px-3 py-2 bg-lotus/20 text-lotus text-xs font-medium rounded-lg border border-lotus/30 hover:bg-lotus/30 transition disabled:opacity-50"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={1.5}
              d="M12 10v6m0 0l-3-3m3 3l3-3m2 8H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
            />
          </svg>
          {csvLoading ? "Downloading..." : "Download CSV"}
        </button>
      </div>

      {/* Summary Cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <StatCard
          label="Total P&L"
          value={`${totalPnL >= 0 ? "+" : ""}${totalPnL.toLocaleString("en-IN", { style: "currency", currency: "INR", maximumFractionDigits: 0 })}`}
          color={totalPnL >= 0 ? "text-profit" : "text-loss"}
        />
        <StatCard
          label="Total Volume"
          value={totalVolume.toLocaleString("en-IN", { style: "currency", currency: "INR", maximumFractionDigits: 0 })}
          color="text-white"
        />
        <StatCard label="Total Bets" value={totalBets.toLocaleString("en-IN")} color="text-white" />
        <StatCard label="Win Rate" value={`${winRate}%`} color="text-amber-400" />
      </div>

      {/* Charts Row */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* P&L Bar Chart */}
        <div className="bg-surface border border-gray-800 rounded-lg p-4">
          <h2 className="text-sm font-semibold text-white mb-4">Daily P&L</h2>
          {barData.length > 0 ? (
            <BarChart data={barData} />
          ) : (
            <div className="h-40 flex items-center justify-center text-xs text-gray-500">
              No P&L data available
            </div>
          )}
        </div>

        {/* Volume by Sport */}
        <div className="bg-surface border border-gray-800 rounded-lg p-4">
          <h2 className="text-sm font-semibold text-white mb-4">Volume by Sport</h2>
          {volumeData.length > 0 ? (
            <VolumeChart data={volumeData} />
          ) : (
            <div className="h-40 flex items-center justify-center text-xs text-gray-500">
              No volume data available
            </div>
          )}
        </div>
      </div>

      {/* Bet Stats */}
      <div className="bg-surface border border-gray-800 rounded-lg p-4">
        <h2 className="text-sm font-semibold text-white mb-3">Bet Breakdown</h2>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <div className="text-center">
            <div className="text-2xl font-bold font-mono text-profit">{wonBets}</div>
            <div className="text-[10px] text-gray-500 uppercase tracking-wider mt-1">Won</div>
          </div>
          <div className="text-center">
            <div className="text-2xl font-bold font-mono text-loss">{lostBets}</div>
            <div className="text-[10px] text-gray-500 uppercase tracking-wider mt-1">Lost</div>
          </div>
          <div className="text-center">
            <div className="text-2xl font-bold font-mono text-white">{totalBets - settledTotal}</div>
            <div className="text-[10px] text-gray-500 uppercase tracking-wider mt-1">Pending</div>
          </div>
          <div className="text-center">
            <div className="text-2xl font-bold font-mono text-amber-400">
              {totalVolume > 0 && totalBets > 0
                ? (totalVolume / totalBets).toLocaleString("en-IN", {
                    style: "currency",
                    currency: "INR",
                    maximumFractionDigits: 0,
                  })
                : "₹0"}
            </div>
            <div className="text-[10px] text-gray-500 uppercase tracking-wider mt-1">Avg Stake</div>
          </div>
        </div>
      </div>

      {/* Recent Settlements */}
      <div className="bg-surface border border-gray-800 rounded-lg p-4">
        <h2 className="text-sm font-semibold text-white mb-3">Recent Settlements</h2>
        {settlements.length > 0 ? (
          <>
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="text-gray-500 border-b border-gray-800">
                  <th className="text-left py-2 pr-3 font-medium">User</th>
                  <th className="text-left py-2 pr-3 font-medium">Market</th>
                  <th className="text-left py-2 pr-3 font-medium">Side</th>
                  <th className="text-right py-2 pr-3 font-medium">Stake</th>
                  <th className="text-right py-2 font-medium">P&L</th>
                </tr>
              </thead>
              <tbody>
                {settlements
                  .slice(
                    (settlementPage - 1) * SETTLEMENTS_PER_PAGE,
                    settlementPage * SETTLEMENTS_PER_PAGE
                  )
                  .map((s) => (
                  <tr
                    key={s.bet_id}
                    className="border-b border-gray-800/50 hover:bg-white/[0.02]"
                  >
                    <td className="py-2 pr-3 text-gray-300">{s.user}</td>
                    <td className="py-2 pr-3 text-gray-400 max-w-[200px] truncate">
                      {s.market}
                    </td>
                    <td className="py-2 pr-3">
                      <span
                        className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${
                          s.side === "back"
                            ? "bg-blue-500/20 text-blue-400"
                            : "bg-pink-500/20 text-pink-400"
                        }`}
                      >
                        {s.side.toUpperCase()}
                      </span>
                    </td>
                    <td className="py-2 pr-3 text-right text-gray-300 font-mono">
                      {s.stake.toLocaleString("en-IN", { style: "currency", currency: "INR" })}
                    </td>
                    <td
                      className={`py-2 text-right font-mono font-medium ${
                        s.pnl >= 0 ? "text-profit" : "text-loss"
                      }`}
                    >
                      {s.pnl >= 0 ? "+" : ""}
                      {s.pnl.toLocaleString("en-IN", { style: "currency", currency: "INR" })}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Pagination
            currentPage={settlementPage}
            totalPages={Math.ceil(settlements.length / SETTLEMENTS_PER_PAGE)}
            onPageChange={setSettlementPage}
          />
          </>
        ) : (
          <div className="text-center py-8 text-xs text-gray-500">
            No settled bets yet
          </div>
        )}
      </div>
    </div>
  );
}
