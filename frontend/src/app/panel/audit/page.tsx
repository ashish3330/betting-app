"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth";
import Select from "@/components/Select";
import { api } from "@/lib/api";
import Pagination from "@/components/Pagination";

interface AuditEntry {
  id: number;
  user_id: number;
  username: string;
  action: string;
  details: string;
  ip: string;
  timestamp: string;
}

const ITEMS_PER_PAGE = 30;

const actionColors: Record<string, string> = {
  login: "bg-blue-500/20 text-blue-400",
  logout: "bg-gray-500/20 text-gray-400",
  bet_placed: "bg-purple-500/20 text-purple-400",
  credit_transfer: "bg-yellow-500/20 text-yellow-400",
  user_created: "bg-green-500/20 text-green-400",
  status_change: "bg-orange-500/20 text-orange-400",
  cashout_accepted: "bg-profit/20 text-profit",
  settlement: "bg-cyan-500/20 text-cyan-400",
  login_failed: "bg-loss/20 text-loss",
  otp_generated: "bg-indigo-500/20 text-indigo-400",
};

export default function AuditLogPage() {
  const { user, isLoggedIn, isLoading: authLoading } = useAuth();
  const router = useRouter();
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [filterAction, setFilterAction] = useState("");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);

  // Auth guard: redirect if not logged in or not admin/superadmin
  useEffect(() => {
    if (authLoading) return;
    if (!isLoggedIn) {
      router.push("/login");
      return;
    }
    const role = user?.role;
    if (role !== "admin" && role !== "superadmin") {
      router.push("/");
      return;
    }
  }, [authLoading, isLoggedIn, user, router]);

  useEffect(() => {
    if (authLoading || !isLoggedIn) return;
    api.request<AuditEntry[]>("/api/v1/panel/audit", { auth: true })
      .then((data) => setEntries(Array.isArray(data) ? data : []))
      .catch(() => setEntries([]))
      .finally(() => setLoading(false));
  }, [authLoading, isLoggedIn]);

  const filtered = entries.filter((e) => {
    if (filterAction && e.action !== filterAction) return false;
    if (search && !e.username?.toLowerCase().includes(search.toLowerCase()) && !e.details?.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

  const totalPages = Math.ceil(filtered.length / ITEMS_PER_PAGE);
  const paginated = filtered.slice((page - 1) * ITEMS_PER_PAGE, page * ITEMS_PER_PAGE);

  const actionTypes = [...new Set(entries.map((e) => e.action))].sort();

  return (
    <div className="space-y-4 max-w-5xl">
      <div>
        <h1 className="text-xl font-bold text-white">Audit Log</h1>
        <p className="text-sm text-gray-500">{entries.length} total entries</p>
      </div>

      {/* Filters */}
      <div className="flex gap-2 flex-wrap">
        <input
          type="text"
          placeholder="Search user or details..."
          value={search}
          onChange={(e) => { setSearch(e.target.value); setPage(1); }}
          className="h-8 px-3 text-xs bg-surface border border-gray-800 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus w-56"
        />
        <Select
          value={filterAction}
          onChange={(v) => { setFilterAction(v); setPage(1); }}
          placeholder="All Actions"
          options={[
            { value: "", label: "All Actions" },
            ...actionTypes.map((a) => ({ value: a, label: a.replace(/_/g, " ") })),
          ]}
          className="w-40"
        />
      </div>

      {/* Table */}
      {loading ? (
        <div className="space-y-2">
          {Array.from({ length: 8 }).map((_, i) => (
            <div key={i} className="h-10 bg-surface rounded animate-pulse" />
          ))}
        </div>
      ) : paginated.length === 0 ? (
        <div className="text-center py-12 text-gray-500 text-sm">No audit entries found</div>
      ) : (
        <div className="bg-surface rounded-lg border border-gray-800/60 overflow-hidden">
          <div className="grid grid-cols-12 gap-1 px-3 py-2 border-b border-gray-800/40 text-[10px] text-gray-500 font-semibold uppercase tracking-wider">
            <div className="col-span-2">Time</div>
            <div className="col-span-2">User</div>
            <div className="col-span-2">Action</div>
            <div className="col-span-4">Details</div>
            <div className="col-span-2">IP</div>
          </div>

          {paginated.map((e) => (
            <div key={e.id} className="grid grid-cols-12 gap-1 px-3 py-2 border-b border-gray-800/20 text-xs items-center hover:bg-surface-light/30">
              <div className="col-span-2 text-gray-500 font-mono text-[10px]">
                {new Date(e.timestamp).toLocaleString("en-IN", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit", second: "2-digit" })}
              </div>
              <div className="col-span-2 text-white font-medium truncate">{e.username || `#${e.user_id}`}</div>
              <div className="col-span-2">
                <span className={`text-[10px] px-1.5 py-0.5 rounded font-medium ${actionColors[e.action] || "bg-gray-700 text-gray-400"}`}>
                  {e.action.replace(/_/g, " ")}
                </span>
              </div>
              <div className="col-span-4 text-gray-400 truncate text-[11px]">{e.details}</div>
              <div className="col-span-2 text-gray-500 font-mono text-[10px]">{e.ip || "-"}</div>
            </div>
          ))}
        </div>
      )}

      {totalPages > 1 && (
        <Pagination currentPage={page} totalPages={totalPages} onPageChange={setPage} />
      )}
    </div>
  );
}
