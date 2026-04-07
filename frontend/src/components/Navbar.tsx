"use client";

import Link from "next/link";
import { useAuth } from "@/lib/auth";
import { useState, useRef, useEffect } from "react";
import { getTheme, toggleTheme, initTheme, type Theme } from "@/lib/theme";
import { api } from "@/lib/api";

interface NavbarProps {
  onToggleSidebar?: () => void;
  sidebarOpen?: boolean;
  liveCount?: number;
}

export default function Navbar({ onToggleSidebar, sidebarOpen = false, liveCount = 0 }: NavbarProps) {
  const { user, isLoggedIn, balance, logout } = useAuth();
  const [accountOpen, setAccountOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [theme, setThemeState] = useState<Theme>("dark");
  const [unreadCount, setUnreadCount] = useState(0);
  const [exposureOpen, setExposureOpen] = useState(false);
  const [exposureBets, setExposureBets] = useState<{ id: string; market_id: string; market_name: string; selection_name: string; side: string; display_side: string; market_type: string; stake: number; price: number; status: string }[]>([]);
  const accountRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLDivElement>(null);
  const exposureRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    initTheme();
    setThemeState(getTheme());
  }, []);

  // Fetch unread notification count
  useEffect(() => {
    if (!isLoggedIn) {
      setUnreadCount(0);
      return;
    }
    let cancelled = false;
    async function fetchUnread() {
      try {
        const notifications = await api.fetchNotifications();
        if (!cancelled && Array.isArray(notifications)) {
          setUnreadCount(notifications.filter((n) => !n.read).length);
        }
      } catch {
        // silent
      }
    }
    fetchUnread();
    const interval = setInterval(fetchUnread, 30000);

    // Listen for instant refresh from notifications page
    const handler = () => fetchUnread();
    window.addEventListener("notifications-read", handler);

    return () => {
      cancelled = true;
      clearInterval(interval);
      window.removeEventListener("notifications-read", handler);
    };
  }, [isLoggedIn]);

  const handleToggleTheme = () => {
    const next = toggleTheme();
    setThemeState(next);
  };

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (
        accountRef.current &&
        !accountRef.current.contains(e.target as Node)
      ) {
        setAccountOpen(false);
      }
      if (
        searchRef.current &&
        !searchRef.current.contains(e.target as Node)
      ) {
        setSearchOpen(false);
      }
      if (
        exposureRef.current &&
        !exposureRef.current.contains(e.target as Node)
      ) {
        setExposureOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClick);
    return () => document.removeEventListener("mousedown", handleClick);
  }, []);

  return (
    <nav
      className="bg-[var(--nav-bg)] border-b border-gray-800/60 sticky top-0 z-50"
      style={{ height: "50px" }}
    >
      <div className="h-full px-1.5 sm:px-3 flex items-center justify-between gap-1 sm:gap-3">
        {/* Left: Hamburger + Logo */}
        <div className="flex items-center gap-2 flex-shrink-0">
          {/* Mobile sidebar toggle */}
          <button
            onClick={onToggleSidebar}
            className="lg:hidden p-1.5 text-gray-400 hover:text-white transition"
            aria-label={sidebarOpen ? "Close menu" : "Open menu"}
          >
            {sidebarOpen ? (
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            ) : (
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
              </svg>
            )}
          </button>

          {/* Logo — switches between dark/light variants */}
          <Link href="/" className="flex items-center">
            <img src={theme === "dark" ? "/logo.svg?v=3" : "/logo-light.svg?v=3"} alt="3XBet" className="h-8 w-auto" />
          </Link>
        </div>

        {/* Center: Live indicator + Search — hidden on mobile */}
        <div className="hidden sm:flex items-center gap-3 flex-1 justify-center max-w-md">
          {/* Live indicator */}
          <div className="flex items-center gap-1.5 flex-shrink-0">
            <span className="relative flex h-2 w-2">
              <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75" />
              <span className="relative inline-flex rounded-full h-2 w-2 bg-green-500" />
            </span>
            <span className="text-xs text-green-400 font-semibold hidden sm:inline">
              Live
            </span>
            {liveCount > 0 && (
              <span className="text-[10px] font-bold bg-green-500/20 text-green-400 px-1.5 py-0.5 rounded-full">
                {liveCount}
              </span>
            )}
          </div>

          {/* Search bar */}
          <div ref={searchRef} className="relative flex-1 max-w-[240px] hidden md:block">
            <div className="relative">
              <svg
                className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-500"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
                />
              </svg>
              <input
                type="text"
                placeholder="Search events..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                onFocus={() => setSearchOpen(true)}
                className="w-full h-8 pl-8 pr-3 text-xs bg-white/5 border border-gray-800/60 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-gray-600 transition"
              />
            </div>
          </div>
        </div>

        {/* Right section */}
        <div className="flex items-center gap-1 sm:gap-2 flex-shrink-0">
          {/* Theme Toggle — hidden on mobile, available in profile dropdown */}
          <button
            onClick={handleToggleTheme}
            className="hidden sm:block p-1 text-gray-400 hover:text-white transition rounded-md hover:bg-white/5"
            aria-label={theme === "dark" ? "Switch to light mode" : "Switch to dark mode"}
            title={theme === "dark" ? "Light mode" : "Dark mode"}
          >
            {theme === "dark" ? (
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" />
              </svg>
            ) : (
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" />
              </svg>
            )}
          </button>

          {isLoggedIn && balance ? (
            <>
              {/* Deposit / Withdraw stacked — visible on all screens */}
              <div className="flex flex-col gap-0.5">
                <Link href="/account/deposit"
                  className="bg-green-600 hover:bg-green-700 text-white text-[8px] sm:text-[10px] font-bold px-1.5 sm:px-2.5 py-0.5 rounded transition text-center leading-tight">
                  Deposit
                </Link>
                <Link href="/account/withdraw"
                  className="bg-red-600 hover:bg-red-700 text-white text-[8px] sm:text-[10px] font-bold px-1.5 sm:px-2.5 py-0.5 rounded transition text-center leading-tight">
                  Withdraw
                </Link>
              </div>

              {/* Balance + Exposure stacked */}
              <div className="flex flex-col items-end leading-tight min-w-0">
                <div className="flex items-center gap-0.5">
                  <span className="text-[9px] text-gray-500 hidden sm:inline">Bal:</span>
                  <span className="text-profit font-bold text-[10px] sm:text-[11px] font-mono truncate max-w-[70px] sm:max-w-none">
                    {"\u20B9"}{balance.available_balance?.toLocaleString("en-IN") ?? "0"}
                  </span>
                </div>
                <div ref={exposureRef} className="relative">
                    <button
                      onClick={async () => {
                        setExposureOpen(!exposureOpen);
                        if (!exposureOpen) {
                          try {
                            const data = await api.request<{ bets: typeof exposureBets } | typeof exposureBets>(
                              "/api/v1/bets?status=open&limit=20", { auth: true }
                            );
                            const bets = Array.isArray(data) ? data : (data as { bets: typeof exposureBets }).bets || [];
                            setExposureBets(bets);
                          } catch { /* silent */ }
                        }
                      }}
                      className="flex items-center gap-0.5"
                    >
                      <span className="text-[9px] text-gray-500">Exp:</span>
                      <span className={`font-bold text-[11px] font-mono ${balance.exposure > 0 ? "text-loss" : "text-gray-500"}`}>
                        {balance.exposure > 0 ? "-" : ""}{"\u20B9"}{balance.exposure?.toLocaleString("en-IN") ?? "0"}
                      </span>
                      <svg className={`w-2.5 h-2.5 text-gray-500 transition-transform ${exposureOpen ? "rotate-180" : ""}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M19 9l-7 7-7-7" />
                      </svg>
                    </button>

                    {/* Exposure breakdown dropdown */}
                    {exposureOpen && (
                      <div className="fixed right-1 sm:right-2 mt-1 bg-[var(--dropdown-bg)] border border-gray-800 rounded-lg shadow-2xl py-1 w-64 sm:w-72 z-[100] max-h-60 overflow-y-auto" style={{ top: "50px" }}>
                        <div className="px-3 py-1.5 border-b border-gray-800/60">
                          <span className="text-[10px] font-bold text-gray-400 uppercase">Exposure Breakdown</span>
                        </div>
                        {exposureBets.length === 0 ? (
                          <p className="px-3 py-3 text-[10px] text-gray-500 text-center">No active bets</p>
                        ) : (() => {
                          const matchBets = exposureBets.filter((b) => b.market_type !== "fancy" && b.market_type !== "session");
                          const fancyBets = exposureBets.filter((b) => b.market_type === "fancy" || b.market_type === "session");
                          const showHeaders = matchBets.length > 0 && fancyBets.length > 0;

                          const renderBet = (bet: typeof exposureBets[0]) => {
                            const isFancy = bet.market_type === "fancy" || bet.market_type === "session";
                            const displaySide = bet.display_side || bet.side;
                            const isYes = displaySide === "yes" || (isFancy && bet.side === "back");
                            return (
                              <Link key={bet.id} href={`/markets/${bet.market_id || bet.id}`} onClick={() => setExposureOpen(false)}
                                className="block px-3 py-1.5 hover:bg-white/5 transition border-b border-gray-800/20 last:border-0">
                                <div className="flex items-center justify-between">
                                  <div className="flex items-center gap-1.5 min-w-0">
                                    {isFancy && <span className="text-[8px] px-1 py-0.5 rounded bg-purple-500/20 text-purple-400 font-bold flex-shrink-0">FANCY</span>}
                                    <span className="text-[10px] text-white truncate max-w-[120px]">{bet.market_name || bet.market_id || bet.id}</span>
                                  </div>
                                  <span className={`text-[10px] px-1 py-0.5 rounded font-medium ${
                                    isFancy
                                      ? isYes ? "bg-[#72BBEF]/20 text-[#72BBEF]" : "bg-[#FAA9BA]/20 text-[#FAA9BA]"
                                      : displaySide === "back" ? "bg-[#72BBEF]/20 text-[#72BBEF]" : "bg-[#FAA9BA]/20 text-[#FAA9BA]"
                                  }`}>
                                    {isFancy ? (isYes ? "YES" : "NO") : displaySide?.toUpperCase()}
                                  </span>
                                </div>
                                <div className="flex items-center justify-between text-[9px] text-gray-500 mt-0.5">
                                  <span>{bet.selection_name || "—"}</span>
                                  <span>₹{bet.stake?.toLocaleString("en-IN")} @ {bet.price}</span>
                                </div>
                              </Link>
                            );
                          };

                          return (
                            <>
                              {matchBets.length > 0 && (
                                <>
                                  {showHeaders && <div className="px-3 py-1 text-[9px] font-bold text-gray-500 uppercase bg-white/[0.02]">Match Bets ({matchBets.length})</div>}
                                  {matchBets.map(renderBet)}
                                </>
                              )}
                              {fancyBets.length > 0 && (
                                <>
                                  {showHeaders && <div className="px-3 py-1 text-[9px] font-bold text-purple-400/70 uppercase bg-purple-500/[0.03] border-t border-gray-800/40">Fancy Bets ({fancyBets.length})</div>}
                                  {fancyBets.map(renderBet)}
                                </>
                              )}
                            </>
                          );
                        })()}
                      </div>
                    )}
                  </div>
              </div>
            </>
          ) : null}

          {/* Notification Bell */}
          {isLoggedIn && (
            <Link
              href="/notifications"
              className="relative p-1 text-gray-400 hover:text-white transition rounded-md hover:bg-white/5"
              aria-label="Notifications"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
              </svg>
              {unreadCount > 0 && (
                <span className="absolute -top-0.5 -right-0.5 min-w-[14px] h-3.5 flex items-center justify-center bg-red-500 text-white text-[8px] font-bold rounded-full px-0.5">
                  {unreadCount > 99 ? "99+" : unreadCount}
                </span>
              )}
            </Link>
          )}

          {isLoggedIn ? (
            <div ref={accountRef} className="relative">
              <button
                onClick={() => setAccountOpen(!accountOpen)}
                className="flex items-center gap-1 px-1 sm:px-2 py-1 rounded-md hover:bg-white/5 transition"
              >
                <div className="w-6 h-6 rounded-full bg-lotus/20 flex items-center justify-center">
                  <span className="text-[10px] font-bold text-lotus">
                    {user?.username?.charAt(0).toUpperCase() || "U"}
                  </span>
                </div>
                <span className="text-xs text-gray-400 hidden md:inline max-w-[80px] truncate">
                  {user?.username}
                </span>
                <svg
                  className={`w-3 h-3 text-gray-500 transition-transform ${
                    accountOpen ? "rotate-180" : ""
                  }`}
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2}
                    d="M19 9l-7 7-7-7"
                  />
                </svg>
              </button>

              {accountOpen && (
                <div className="fixed right-2 mt-1 bg-[var(--dropdown-bg)] border border-gray-800 rounded-lg shadow-2xl py-1 w-52 z-[100]" style={{ top: "50px" }}>
                  {/* User info header */}
                  <div className="px-3 py-2 border-b border-gray-800/60">
                    <div className="text-xs font-medium text-white">
                      {user?.username}
                    </div>
                    <div className="text-[10px] text-gray-500">
                      {user?.email}
                    </div>
                  </div>

                  {/* Theme toggle inside dropdown for mobile access */}
                  <button
                    onClick={() => { handleToggleTheme(); setAccountOpen(false); }}
                    className="w-full flex items-center gap-2 text-xs text-gray-400 hover:text-white hover:bg-white/5 px-3 py-2 transition"
                  >
                    {theme === "dark" ? (
                      <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" /></svg>
                    ) : (
                      <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" /></svg>
                    )}
                    {theme === "dark" ? "Light Mode" : "Dark Mode"}
                  </button>

                  <div className="border-t border-gray-800 my-1" />

                  <DropdownLink
                    href="/account"
                    onClick={() => setAccountOpen(false)}
                  >
                    Account Settings
                  </DropdownLink>
                  <DropdownLink
                    href="/account/deposit"
                    onClick={() => setAccountOpen(false)}
                  >
                    Deposit
                  </DropdownLink>
                  <DropdownLink
                    href="/account/withdraw"
                    onClick={() => setAccountOpen(false)}
                  >
                    Withdraw
                  </DropdownLink>
                  <DropdownLink
                    href="/bets"
                    onClick={() => setAccountOpen(false)}
                  >
                    My Bets
                  </DropdownLink>
                  <DropdownLink
                    href="/account/history"
                    onClick={() => setAccountOpen(false)}
                  >
                    Betting History
                  </DropdownLink>
                  <DropdownLink
                    href="/wallet"
                    onClick={() => setAccountOpen(false)}
                  >
                    Wallet / Ledger
                  </DropdownLink>
                  <DropdownLink
                    href="/notifications"
                    onClick={() => setAccountOpen(false)}
                  >
                    Notifications
                  </DropdownLink>

                  {user?.role !== "client" && (
                    <DropdownLink
                      href="/panel"
                      onClick={() => setAccountOpen(false)}
                    >
                      {user?.role === "superadmin" ? "Super Admin Panel" :
                       user?.role === "admin" ? "Admin Panel" :
                       user?.role === "master" ? "Master Panel" : "Agent Panel"}
                    </DropdownLink>
                  )}

                  <div className="border-t border-gray-800 my-1" />
                  <button
                    onClick={() => {
                      setAccountOpen(false);
                      logout();
                    }}
                    className="block w-full text-left text-xs text-loss hover:bg-white/5 px-3 py-2 transition"
                  >
                    Logout
                  </button>
                </div>
              )}
            </div>
          ) : (
            <div className="flex items-center gap-1.5">
              <Link
                href="/login"
                className="text-[11px] bg-lotus hover:bg-lotus-light text-white px-3 py-1.5 rounded-md transition font-semibold"
              >
                Login
              </Link>
              <Link
                href="/register"
                className="text-[11px] bg-white/5 hover:bg-white/10 text-gray-300 px-3 py-1.5 rounded-md transition hidden sm:block"
              >
                Register
              </Link>
            </div>
          )}

        </div>
      </div>

      {/* Mobile search dropdown */}
      {searchOpen && (
        <div className="md:hidden absolute top-[50px] left-0 right-0 bg-[var(--nav-bg)] border-b border-gray-800/60 px-3 py-2 z-50">
          <input
            type="text"
            placeholder="Search events..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            autoFocus
            className="w-full h-9 px-3 text-xs bg-white/5 border border-gray-800/60 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-gray-600"
          />
        </div>
      )}
    </nav>
  );
}

function DropdownLink({
  href,
  children,
  onClick,
}: {
  href: string;
  children: React.ReactNode;
  onClick: () => void;
}) {
  return (
    <Link
      href={href}
      onClick={onClick}
      className="block text-xs text-gray-400 hover:text-white hover:bg-white/5 px-3 py-2 transition"
    >
      {children}
    </Link>
  );
}
