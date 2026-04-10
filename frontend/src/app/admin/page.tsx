"use client";

import { useEffect, useState } from "react";
import { api, AdminDashboard } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useRouter } from "next/navigation";

export default function AdminPage() {
  const { user, isLoggedIn, isLoading: authLoading } = useAuth();
  const router = useRouter();
  const [dashboard, setDashboard] = useState<AdminDashboard | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!authLoading && (!isLoggedIn || user?.role !== "admin")) {
      router.push("/");
      return;
    }
    if (isLoggedIn && user?.role === "admin") {
      loadDashboard();
      const interval = setInterval(loadDashboard, 30000);
      return () => clearInterval(interval);
    }
  }, [isLoggedIn, authLoading, user, router]);

  async function loadDashboard() {
    try {
      const data = await api.getDashboard();
      setDashboard(data);
    } catch {
      // API not available
    } finally {
      setLoading(false);
    }
  }

  if (authLoading || loading) {
    return (
      <div className="max-w-5xl mx-auto px-3 py-4 space-y-4">
        <div className="bg-surface rounded-xl border border-gray-800 h-12 animate-pulse" />
        <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="bg-surface rounded-xl border border-gray-800 h-28 animate-pulse" />
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto px-3 py-4 space-y-6">
      <div>
        <h1 className="text-lg font-bold text-white">Admin Dashboard</h1>
        <p className="text-xs text-gray-500">
          Real-time platform overview
        </p>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
        <StatCard
          label="Active Users"
          value={dashboard?.active_users?.toString() || "0"}
          icon={
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0zm6 3a2 2 0 11-4 0 2 2 0 014 0zM7 10a2 2 0 11-4 0 2 2 0 014 0z" />
            </svg>
          }
          accent="text-back"
        />
        <StatCard
          label="Bets Today"
          value={dashboard?.total_bets_today?.toLocaleString("en-IN") || "0"}
          icon={
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
            </svg>
          }
          accent="text-lotus"
        />
        <StatCard
          label="Volume Today"
          value={`\u20B9${formatLargeNumber(dashboard?.total_volume_today || 0)}`}
          icon={
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M13 7h8m0 0v8m0-8l-8 8-4-4-6 6" />
            </svg>
          }
          accent="text-profit"
        />
        <StatCard
          label="Revenue Today"
          value={`\u20B9${formatLargeNumber(dashboard?.revenue_today || 0)}`}
          icon={
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          }
          accent="text-amber-400"
        />
        <StatCard
          label="Markets Live"
          value={dashboard?.active_markets?.toString() || "0"}
          icon={
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M5.636 18.364a9 9 0 010-12.728m12.728 0a9 9 0 010 12.728M9.172 15.828a4 4 0 010-7.656m5.656 0a4 4 0 010 7.656" />
            </svg>
          }
          accent="text-profit"
        />
        <StatCard
          label="Total Exposure"
          value={`\u20B9${formatLargeNumber(dashboard?.total_exposure || 0)}`}
          icon={
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
          }
          accent="text-loss"
        />
      </div>

      {/* Quick Actions */}
      <div className="bg-surface rounded-xl border border-gray-800 p-4">
        <h2 className="text-sm font-medium text-gray-400 mb-3">
          Quick Actions
        </h2>
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
          <ActionButton label="Refresh Data" onClick={loadDashboard} />
          <ActionButton label="View Markets" onClick={() => router.push("/markets")} />
          <ActionButton label="System Health" onClick={() => window.open("/health", "_blank")} />
          <ActionButton label="Back to Home" onClick={() => router.push("/")} />
        </div>
      </div>
    </div>
  );
}

function StatCard({
  label,
  value,
  icon,
  accent,
}: {
  label: string;
  value: string;
  icon: React.ReactNode;
  accent: string;
}) {
  return (
    <div className="bg-surface rounded-xl border border-gray-800 p-4">
      <div className="flex items-center justify-between mb-3">
        <span className={`${accent} opacity-60`}>{icon}</span>
      </div>
      <div className={`text-xl font-bold font-mono ${accent}`}>{value}</div>
      <div className="text-[10px] text-gray-500 mt-1 uppercase tracking-wide">
        {label}
      </div>
    </div>
  );
}

function ActionButton({
  label,
  onClick,
}: {
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className="bg-surface-lighter hover:bg-gray-600 text-gray-300 text-xs font-medium px-3 py-2 rounded-lg transition"
    >
      {label}
    </button>
  );
}

function formatLargeNumber(n: number): string {
  if (n >= 10000000) return `${(n / 10000000).toFixed(2)}Cr`;
  if (n >= 100000) return `${(n / 100000).toFixed(2)}L`;
  if (n >= 1000) return `${(n / 1000).toFixed(1)}K`;
  return n.toFixed(0);
}
