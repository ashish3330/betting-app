"use client";

import { useEffect, useMemo, useState } from "react";
import { useAuth } from "@/lib/auth";
import { api, Notification } from "@/lib/api";
import Link from "next/link";

type TypeFilter = "all" | "bets" | "wallet" | "promotions" | "system";

const TYPE_TABS: { id: TypeFilter; label: string }[] = [
  { id: "all", label: "All" },
  { id: "bets", label: "Bets" },
  { id: "wallet", label: "Wallet" },
  { id: "promotions", label: "Promotions" },
  { id: "system", label: "System" },
];

function classifyType(t: string): TypeFilter {
  const type = t.toLowerCase();
  if (type.startsWith("bet") || type === "cashout") return "bets";
  if (type.includes("deposit") || type.includes("withdraw") || type === "credit" || type === "debit") return "wallet";
  if (type === "promotion" || type === "bonus" || type === "referral") return "promotions";
  return "system";
}

function NotificationGlyph({ type }: { type: string }) {
  const cls = "w-4 h-4";
  const t = type.toLowerCase();
  if (t === "bet_won" || t === "bet_settled") {
    // trophy
    return (
      <svg className={cls} fill="none" stroke="currentColor" strokeWidth={1.75} viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" d="M16.5 18.75h-9m9 0a3 3 0 0 1 3 3h-15a3 3 0 0 1 3-3m9 0v-3.375c0-.621-.503-1.125-1.125-1.125h-.871M7.5 18.75v-3.375c0-.621.504-1.125 1.125-1.125h.872m5.007 0H9.497m5.007 0a7.454 7.454 0 0 1-.982-3.172M9.497 14.25a7.454 7.454 0 0 0 .981-3.172M5.25 4.236c-.982.143-1.954.317-2.916.52A6.003 6.003 0 0 0 7.73 9.728M5.25 4.236V4.5c0 2.108.966 3.99 2.48 5.228M5.25 4.236V2.721C7.456 2.41 9.71 2.25 12 2.25c2.291 0 4.545.16 6.75.47v1.516M7.73 9.728a6.726 6.726 0 0 0 2.748 1.35m8.272-6.842V4.5c0 2.108-.966 3.99-2.48 5.228m2.48-5.492a46.32 46.32 0 0 1 2.916.52 6.003 6.003 0 0 1-5.395 4.972m0 0a6.726 6.726 0 0 1-2.749 1.35m0 0a6.772 6.772 0 0 1-3.044 0" />
      </svg>
    );
  }
  if (t === "bet_placed") {
    // ticket
    return (
      <svg className={cls} fill="none" stroke="currentColor" strokeWidth={1.75} viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" d="M16.5 6v.75m0 3v.75m0 3v.75m0 3V18m-9-5.25h5.25M7.5 15h3M3.375 5.25c-.621 0-1.125.504-1.125 1.125v3.026a2.999 2.999 0 0 1 0 5.198v3.026c0 .621.504 1.125 1.125 1.125h17.25c.621 0 1.125-.504 1.125-1.125v-3.026a2.999 2.999 0 0 1 0-5.198V6.375c0-.621-.504-1.125-1.125-1.125H3.375Z" />
      </svg>
    );
  }
  if (t === "bet_lost") {
    // ticket with X
    return (
      <svg className={cls} fill="none" stroke="currentColor" strokeWidth={1.75} viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" d="M9 9l6 6m0-6-6 6M3.375 5.25c-.621 0-1.125.504-1.125 1.125v3.026a2.999 2.999 0 0 1 0 5.198v3.026c0 .621.504 1.125 1.125 1.125h17.25c.621 0 1.125-.504 1.125-1.125v-3.026a2.999 2.999 0 0 1 0-5.198V6.375c0-.621-.504-1.125-1.125-1.125H3.375Z" />
      </svg>
    );
  }
  if (t === "deposit" || t === "deposit_request" || t === "credit") {
    // arrow-down-circle
    return (
      <svg className={cls} fill="none" stroke="currentColor" strokeWidth={1.75} viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v6m0 0 3-3m-3 3-3-3m12 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />
      </svg>
    );
  }
  if (t === "withdrawal" || t === "debit") {
    // arrow-up-circle
    return (
      <svg className={cls} fill="none" stroke="currentColor" strokeWidth={1.75} viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" d="M12 15V9m0 0 3 3m-3-3-3 3m12 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />
      </svg>
    );
  }
  if (t === "promotion" || t === "bonus" || t === "referral") {
    // gift
    return (
      <svg className={cls} fill="none" stroke="currentColor" strokeWidth={1.75} viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" d="M21 11.25v8.25a1.5 1.5 0 0 1-1.5 1.5H5.25a1.5 1.5 0 0 1-1.5-1.5v-8.25M12 4.875A3.375 3.375 0 1 0 15.375 8.25H12V4.875ZM12 4.875A3.375 3.375 0 1 1 8.625 8.25H12V4.875ZM12 4.875v14.25m-9-9h18" />
      </svg>
    );
  }
  if (t === "login") {
    // user
    return (
      <svg className={cls} fill="none" stroke="currentColor" strokeWidth={1.75} viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" d="M15.75 6a3.75 3.75 0 1 1-7.5 0 3.75 3.75 0 0 1 7.5 0ZM4.501 20.118a7.5 7.5 0 0 1 14.998 0A17.933 17.933 0 0 1 12 21.75c-2.676 0-5.216-.584-7.499-1.632Z" />
      </svg>
    );
  }
  // info circle (default / system / alert)
  return (
    <svg className={cls} fill="none" stroke="currentColor" strokeWidth={1.75} viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" d="m11.25 11.25.041-.02a.75.75 0 0 1 1.063.852l-.708 2.836a.75.75 0 0 0 1.063.853l.041-.021M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9-3.75h.008v.008H12V8.25Z" />
    </svg>
  );
}

function notificationColorClass(type: string) {
  const t = type.toLowerCase();
  if (t === "bet_placed") return "bg-blue-500/20 text-blue-400";
  if (t === "bet_won" || t === "bet_settled") return "bg-profit/20 text-profit";
  if (t === "bet_lost") return "bg-loss/20 text-loss";
  if (t === "deposit" || t === "deposit_request") return "bg-green-500/20 text-green-400";
  if (t === "credit") return "bg-amber-500/20 text-amber-400";
  if (t === "withdrawal") return "bg-orange-500/20 text-orange-400";
  if (t === "login") return "bg-purple-500/20 text-purple-400";
  if (t === "promotion" || t === "bonus" || t === "referral") return "bg-lotus/20 text-lotus";
  if (t === "alert" || t === "system") return "bg-loss/20 text-loss";
  return "bg-gray-500/20 text-gray-400";
}

function bucketForDate(iso: string): "Today" | "Yesterday" | "Earlier this week" | "Older" {
  const created = new Date(iso);
  const now = new Date();
  const startToday = new Date(now);
  startToday.setHours(0, 0, 0, 0);
  const startYesterday = new Date(startToday);
  startYesterday.setDate(startYesterday.getDate() - 1);
  const startWeek = new Date(startToday);
  startWeek.setDate(startWeek.getDate() - 7);

  if (created >= startToday) return "Today";
  if (created >= startYesterday) return "Yesterday";
  if (created >= startWeek) return "Earlier this week";
  return "Older";
}

const BUCKET_ORDER: Array<"Today" | "Yesterday" | "Earlier this week" | "Older"> = [
  "Today",
  "Yesterday",
  "Earlier this week",
  "Older",
];

export default function NotificationsPage() {
  const { isLoggedIn } = useAuth();
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [loading, setLoading] = useState(true);
  const [typeFilter, setTypeFilter] = useState<TypeFilter>("all");

  useEffect(() => {
    if (isLoggedIn) loadNotifications();
  }, [isLoggedIn]);

  async function loadNotifications() {
    try {
      const data = await api.fetchNotifications();
      setNotifications(Array.isArray(data) ? data : []);
    } catch {
      // silent
    } finally {
      setLoading(false);
    }
  }

  async function markRead(id: string) {
    try {
      await api.markNotificationRead(id);
      setNotifications((prev) =>
        prev.map((n) => (n.id === id ? { ...n, read: true } : n))
      );
      window.dispatchEvent(new Event("notifications-read"));
    } catch {
      // silent
    }
  }

  async function markAllRead() {
    try {
      await api.markAllNotificationsRead();
      setNotifications((prev) => prev.map((n) => ({ ...n, read: true })));
      window.dispatchEvent(new Event("notifications-read"));
    } catch {
      // silent
    }
  }

  const unreadCount = notifications.filter((n) => !n.read).length;

  const filtered = useMemo(() => {
    if (typeFilter === "all") return notifications;
    return notifications.filter((n) => classifyType(n.type) === typeFilter);
  }, [notifications, typeFilter]);

  const grouped = useMemo(() => {
    const groups: Record<string, Notification[]> = {};
    for (const n of filtered) {
      const bucket = bucketForDate(n.created_at);
      (groups[bucket] ||= []).push(n);
    }
    return groups;
  }, [filtered]);

  if (!isLoggedIn) {
    return (
      <div className="max-w-7xl mx-auto px-3 py-16 text-center">
        <h2 className="text-lg font-bold text-white">Please Login</h2>
        <p className="text-sm text-gray-500 mt-1">You need to be logged in to view notifications.</p>
        <Link href="/login" className="inline-block mt-4 bg-lotus hover:bg-lotus-light text-white px-6 py-2 rounded-lg text-sm font-medium transition">
          Login
        </Link>
      </div>
    );
  }

  return (
    <div className="max-w-3xl mx-auto px-3 py-4 space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-bold text-white">Notifications</h1>
          <p className="text-xs text-gray-500">
            {unreadCount > 0 ? `${unreadCount} unread` : "All caught up"}
          </p>
        </div>
        {unreadCount > 0 && (
          <button
            onClick={markAllRead}
            className="text-xs text-lotus hover:text-lotus-light transition"
          >
            Mark all as read
          </button>
        )}
      </div>

      {/* Type filter tabs */}
      <div className="flex gap-1 bg-surface rounded-lg p-0.5 w-fit overflow-x-auto">
        {TYPE_TABS.map((t) => (
          <button
            key={t.id}
            onClick={() => setTypeFilter(t.id)}
            className={`px-3 py-1.5 rounded-md text-xs font-medium transition whitespace-nowrap ${
              typeFilter === t.id
                ? "bg-surface-lighter text-white"
                : "text-gray-500 hover:text-gray-300"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {loading ? (
        <div className="space-y-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="h-20 bg-surface rounded-xl border border-gray-800 animate-pulse" />
          ))}
        </div>
      ) : filtered.length === 0 ? (
        <div className="text-center py-16">
          <svg className="w-16 h-16 mx-auto text-gray-400 mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1} d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
          </svg>
          <h3 className="text-lg font-medium text-gray-400">No Notifications</h3>
          <p className="text-sm text-gray-400 mt-1">
            You will receive notifications about bets, deposits, and promotions here.
          </p>
        </div>
      ) : (
        <div className="space-y-5">
          {BUCKET_ORDER.filter((b) => grouped[b]?.length).map((bucket) => (
            <section key={bucket} className="space-y-2">
              <h2 className="text-[10px] font-semibold text-gray-500 uppercase tracking-wider px-1">
                {bucket}
              </h2>
              <div className="space-y-2">
                {grouped[bucket].map((notification) => (
                  <button
                    key={notification.id}
                    onClick={() => !notification.read && markRead(notification.id)}
                    className={`w-full text-left bg-surface rounded-xl border p-4 transition ${
                      notification.read
                        ? "border-gray-800 opacity-60"
                        : "border-gray-700 hover:border-gray-600"
                    }`}
                  >
                    <div className="flex gap-3">
                      <div
                        className={`w-9 h-9 rounded-full flex items-center justify-center flex-shrink-0 ${notificationColorClass(
                          notification.type
                        )}`}
                      >
                        <NotificationGlyph type={notification.type} />
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="flex items-start justify-between gap-2">
                          <h3 className="text-sm font-medium text-white truncate">
                            {notification.title}
                          </h3>
                          {!notification.read && (
                            <span className="w-2 h-2 bg-lotus rounded-full flex-shrink-0 mt-1.5" />
                          )}
                        </div>
                        <p className="text-xs text-gray-400 mt-0.5">{notification.message}</p>
                        <p className="text-[10px] text-gray-400 mt-1">
                          {new Date(notification.created_at).toLocaleString("en-IN")}
                        </p>
                      </div>
                    </div>
                  </button>
                ))}
              </div>
            </section>
          ))}
        </div>
      )}
    </div>
  );
}
