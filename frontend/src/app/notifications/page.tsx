"use client";

import { useEffect, useState } from "react";
import { useAuth } from "@/lib/auth";
import { api, Notification } from "@/lib/api";
import Link from "next/link";

export default function NotificationsPage() {
  const { isLoggedIn } = useAuth();
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [loading, setLoading] = useState(true);

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

  const notificationIcon = (type: string) => {
    switch (type) {
      case "bet_placed": return "bg-blue-500/20 text-blue-400";
      case "bet_won": case "bet_settled": return "bg-profit/20 text-profit";
      case "bet_lost": return "bg-loss/20 text-loss";
      case "deposit": case "deposit_request": return "bg-green-500/20 text-green-400";
      case "credit": return "bg-amber-500/20 text-amber-400";
      case "withdrawal": return "bg-orange-500/20 text-orange-400";
      case "login": return "bg-purple-500/20 text-purple-400";
      case "promotion": return "bg-lotus/20 text-lotus";
      case "alert": case "system": return "bg-loss/20 text-loss";
      default: return "bg-gray-500/20 text-gray-400";
    }
  };

  const notificationIconLetter = (type: string) => {
    switch (type) {
      case "bet_placed": return "B";
      case "bet_won": case "bet_settled": return "W";
      case "bet_lost": return "L";
      case "deposit": case "deposit_request": return "D";
      case "credit": return "C";
      case "withdrawal": return "W";
      case "login": return "U";
      case "promotion": return "P";
      case "alert": case "system": return "!";
      default: return "N";
    }
  };

  return (
    <div className="max-w-3xl mx-auto px-3 py-4 space-y-6">
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

      {loading ? (
        <div className="space-y-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="h-20 bg-surface rounded-xl border border-gray-800 animate-pulse" />
          ))}
        </div>
      ) : notifications.length === 0 ? (
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
        <div className="space-y-2">
          {notifications.map((notification) => (
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
                <div className={`w-9 h-9 rounded-full flex items-center justify-center flex-shrink-0 text-xs font-bold ${notificationIcon(notification.type)}`}>
                  {notificationIconLetter(notification.type)}
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
      )}
    </div>
  );
}
