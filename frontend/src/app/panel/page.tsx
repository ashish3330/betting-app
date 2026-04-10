"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth";
import { api } from "@/lib/api";

interface DashboardStats {
  role: string;
  username: string;
  own_balance: number;
  own_exposure: number;
  direct_children: number;
  total_users: number;
  total_balance: number;
  total_exposure: number;
  users_by_role: Record<string, number>;
  today_bets: number;
  platform_total_users?: number;
  platform_total_markets?: number;
  platform_total_bets?: number;
  platform_total_volume?: number;
}

export default function PanelDashboard() {
  const { user, isLoggedIn, isLoading: authLoading } = useAuth();
  const router = useRouter();
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [loading, setLoading] = useState(true);

  // Auth guard: redirect if not logged in or not admin/superadmin
  useEffect(() => {
    if (authLoading) return;
    if (!isLoggedIn) {
      router.push("/login");
      return;
    }
    const role = user?.role;
    if (role !== "admin" && role !== "superadmin" && role !== "master" && role !== "agent") {
      router.push("/");
      return;
    }
  }, [authLoading, isLoggedIn, user, router]);

  useEffect(() => {
    if (authLoading || !isLoggedIn) return;
    api.request<DashboardStats>("/api/v1/panel/dashboard", { auth: true })
      .then(setStats)
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [authLoading, isLoggedIn]);

  if (loading) {
    return (
      <div className="space-y-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="h-24 bg-surface rounded-lg border border-gray-800 animate-pulse" />
        ))}
      </div>
    );
  }

  if (!stats) return <div className="text-gray-500">Failed to load dashboard</div>;

  const role = user?.role || "";
  const isSA = role === "superadmin";

  const roleAllowed: Record<string, string[]> = {
    superadmin: ["admin", "master", "agent", "client"],
    admin: ["master", "agent", "client"],
    master: ["agent", "client"],
    agent: ["client"],
  };

  return (
    <div className="space-y-6 max-w-5xl">
      {/* Header */}
      <div>
        <h1 className="text-xl font-bold text-white">Dashboard</h1>
        <p className="text-sm text-gray-500 capitalize">{role} Panel</p>
      </div>

      {/* Own Stats */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        <StatCard label="Your Balance" value={`${stats.own_balance.toLocaleString("en-IN")}`} variant="positive" />
        <StatCard label="Your Exposure" value={`${stats.own_exposure.toLocaleString("en-IN")}`} variant="negative" />
        <StatCard label="Direct Children" value={stats.direct_children.toString()} variant="default" />
        <StatCard label="Total Downline" value={stats.total_users.toString()} variant="default" />
      </div>

      {/* Platform Stats (SuperAdmin only) */}
      {isSA && stats.platform_total_users !== undefined && (
        <div>
          <h2 className="text-xs font-semibold text-gray-500 mb-2 uppercase tracking-wider">Platform Overview</h2>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <StatCard label="Total Users" value={stats.platform_total_users?.toString() || "0"} variant="default" />
            <StatCard label="Total Markets" value={stats.platform_total_markets?.toString() || "0"} variant="default" />
            <StatCard label="Total Bets" value={stats.platform_total_bets?.toString() || "0"} variant="default" />
            <StatCard label="Total Volume" value={`${(stats.platform_total_volume || 0).toLocaleString("en-IN")}`} variant="positive" />
          </div>
        </div>
      )}

      {/* Downline Stats */}
      <div>
        <h2 className="text-xs font-semibold text-gray-500 mb-2 uppercase tracking-wider">Downline Summary</h2>
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
          <StatCard label="Downline Balance" value={`${stats.total_balance.toLocaleString("en-IN")}`} variant="default" />
          <StatCard label="Downline Exposure" value={`${stats.total_exposure.toLocaleString("en-IN")}`} variant="negative" />
          <StatCard label="Today Bets" value={stats.today_bets.toString()} variant="default" />
          {Object.entries(stats.users_by_role || {}).map(([r, count]) => (
            <StatCard key={r} label={`${r}s`} value={count.toString()} variant="default" />
          ))}
        </div>
      </div>

      {/* Quick Actions */}
      <div>
        <h2 className="text-xs font-semibold text-gray-500 mb-2 uppercase tracking-wider">Quick Actions</h2>
        <div className="flex flex-wrap gap-2">
          <ActionButton href="/panel/create-user" label={`Create ${(roleAllowed[role] || [])[0] || "User"}`} />
          <ActionButton href="/panel/users" label="View Downline" />
          <ActionButton href="/panel/credit" label="Transfer Credit" />
          <ActionButton href="/" label="Go to Exchange" />
        </div>
      </div>
    </div>
  );
}

function StatCard({ label, value, variant }: { label: string; value: string; variant: "positive" | "negative" | "default" }) {
  const valueColor =
    variant === "positive"
      ? "text-green-400"
      : variant === "negative"
      ? "text-red-400"
      : "text-white";

  return (
    <div className="bg-surface rounded-lg border border-gray-800 p-3">
      <div className="text-[10px] text-gray-500 uppercase tracking-wider">{label}</div>
      <div className={`text-lg font-bold font-mono mt-0.5 ${valueColor}`}>{value}</div>
    </div>
  );
}

function ActionButton({ href, label }: { href: string; label: string }) {
  return (
    <a
      href={href}
      className="flex items-center gap-1.5 bg-surface hover:bg-surface-light border border-gray-800 rounded-lg px-3 py-2 text-xs text-gray-300 hover:text-white transition"
    >
      {label}
    </a>
  );
}
