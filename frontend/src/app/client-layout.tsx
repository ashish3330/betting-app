"use client";

import { AuthProvider } from "@/lib/auth";
import { BetSlipProvider } from "@/lib/betslip";
import Navbar from "@/components/Navbar";
import WhatsAppWidget from "@/components/WhatsAppWidget";
import AgeGate from "@/components/AgeGate";
import DisclaimerBanner from "@/components/DisclaimerBanner";
import Footer from "@/components/Footer";
import { ToastProvider } from "@/components/Toast";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import OfflineBanner from "@/components/OfflineBanner";
import GlobalBetSlip from "@/components/GlobalBetSlip";
import MobileNav from "@/components/MobileNav";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState, useEffect } from "react";
import { api, Competition } from "@/lib/api";

export default function ClientLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const pathname = usePathname();
  const [sidebarOpen, setSidebarOpen] = useState(false);

  // Close sidebar on any navigation (pathname or search params)
  useEffect(() => {
    setSidebarOpen(false);
  }, [pathname]);

  // Also close on hash/search changes (for league links that only change ?competition=)
  useEffect(() => {
    const handleClick = () => {
      // Small delay to let navigation happen first
      setTimeout(() => setSidebarOpen(false), 100);
    };
    const links = document.querySelectorAll('aside a');
    links.forEach(l => l.addEventListener('click', handleClick));
    return () => links.forEach(l => l.removeEventListener('click', handleClick));
  });

  // Close sidebar on escape key
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setSidebarOpen(false);
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  // Prevent body scroll when sidebar is open on mobile
  useEffect(() => {
    if (sidebarOpen) {
      document.body.style.overflow = "hidden";
    } else {
      document.body.style.overflow = "";
    }
    return () => { document.body.style.overflow = ""; };
  }, [sidebarOpen]);

  const showSidebar = !pathname?.startsWith("/login") && !pathname?.startsWith("/register");

  return (
    <AuthProvider>
      <ToastProvider>
      <BetSlipProvider>
      <ErrorBoundary>
      <AgeGate>
      <DisclaimerBanner />
      <Navbar
        onToggleSidebar={() => setSidebarOpen(!sidebarOpen)}
        sidebarOpen={sidebarOpen}
      />
      <OfflineBanner />

      {/* Scrolling announcement ticker — like playzone9 */}
      <div className="bg-[#1a6fb5] overflow-hidden h-7 flex items-center relative dark-section">
        <div className="animate-marquee whitespace-nowrap flex items-center gap-8 text-white text-[12px] font-medium">
          <span>🚀🎉 The Game Begins Now!</span>
          <span>🏆 IPL 2026 is Live — Place your bets now!</span>
          <span>💰 Instant deposits via UPI</span>
          <span>🎰 New Casino Games Added</span>
          <span>⚡ Best odds guaranteed</span>
          <span>🏏 Live Cricket Betting 24/7</span>
          <span>🔥 Welcome Bonus — Deposit Now!</span>
          <span>🚀🎉 The Game Begins Now!</span>
          <span>🏆 IPL 2026 is Live — Place your bets now!</span>
          <span>💰 Instant deposits via UPI</span>
          <span>🎰 New Casino Games Added</span>
          <span>⚡ Best odds guaranteed</span>
        </div>
      </div>

      {/* Live match ticker — scrolling match names like playzone9 */}
      <div className="bg-[#2a2d3a] overflow-hidden h-8 flex items-center border-b border-gray-800/40 dark-section">
        <div className="animate-marquee whitespace-nowrap flex items-center gap-6 text-[11px]">
          {/* These would be dynamic in production */}
          <span className="flex items-center gap-1.5 text-gray-300">🏏 <span className="text-white font-medium">Indian Premier League</span></span>
          <span className="flex items-center gap-1.5 text-gray-300">⚽ <span className="text-white font-medium">Arsenal v Chelsea</span> <span className="text-green-400 text-[10px]">LIVE</span></span>
          <span className="flex items-center gap-1.5 text-gray-300">🎾 <span className="text-white font-medium">Djokovic v Alcaraz</span> <span className="text-green-400 text-[10px]">LIVE</span></span>
          <span className="flex items-center gap-1.5 text-gray-300">🏀 <span className="text-white font-medium">Lakers v Celtics</span></span>
          <span className="flex items-center gap-1.5 text-gray-300">🥊 <span className="text-white font-medium">Fury v Usyk III</span></span>
          <span className="flex items-center gap-1.5 text-gray-300">🏏 <span className="text-white font-medium">MI v CSK</span> <span className="text-green-400 text-[10px]">LIVE</span></span>
          <span className="flex items-center gap-1.5 text-gray-300">⚽ <span className="text-white font-medium">Man Utd v Liverpool</span></span>
          <span className="flex items-center gap-1.5 text-gray-300">🏏 <span className="text-white font-medium">Indian Premier League</span></span>
          <span className="flex items-center gap-1.5 text-gray-300">⚽ <span className="text-white font-medium">Arsenal v Chelsea</span> <span className="text-green-400 text-[10px]">LIVE</span></span>
          <span className="flex items-center gap-1.5 text-gray-300">🎾 <span className="text-white font-medium">Djokovic v Alcaraz</span></span>
        </div>
      </div>

      <div className="flex min-h-[calc(100vh-50px)]">
        {/* ===== Mobile Sidebar Overlay ===== */}
        {showSidebar && sidebarOpen && (
          <div
            className="lg:hidden fixed inset-0 bg-black/60 z-40"
            onClick={() => setSidebarOpen(false)}
          />
        )}

        {/* ===== Sidebar ===== */}
        {showSidebar && (
          <aside
            className={`
              fixed lg:sticky top-[50px] left-0 z-40
              w-[260px] flex-shrink-0
              bg-[var(--bg-surface)] border-r border-gray-800/60
              overflow-y-auto
              h-[calc(100vh-50px)]
              transition-transform duration-300 ease-in-out
              ${sidebarOpen ? "translate-x-0" : "-translate-x-full lg:translate-x-0"}
            `}
          >
            <div className="py-2">
              {/* Mobile close button */}
              <div className="lg:hidden flex items-center justify-between px-4 py-2 border-b border-gray-800/40 mb-1">
                <span className="text-sm font-bold text-white">Menu</span>
                <button
                  onClick={() => setSidebarOpen(false)}
                  className="p-1 text-gray-400 hover:text-white"
                >
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>

              {/* Sports Section — expandable with leagues */}
              <SidebarSection title="Sports">
                <SportTree sport="cricket" label="Cricket" icon="🏏" pathname={pathname} />
                <SportTree sport="football" label="Football" icon="⚽" pathname={pathname} />
                <SportTree sport="tennis" label="Tennis" icon="🎾" pathname={pathname} />
                <SportTree sport="basketball" label="Basketball" icon="🏀" pathname={pathname} />
                <SportTree sport="ice_hockey" label="Ice Hockey" icon="🏒" pathname={pathname} />
                <SportTree sport="baseball" label="Baseball" icon="⚾" pathname={pathname} />
                <SportTree sport="boxing" label="Boxing" icon="🥊" pathname={pathname} />
                <SportTree sport="mma" label="MMA" icon="🤼" pathname={pathname} />
                <SportTree sport="kabaddi" label="Kabaddi" icon="🤾" pathname={pathname} />
                <SportTree sport="horse_racing" label="Horse Racing" icon="🏇" pathname={pathname} />
              </SidebarSection>

              {/* Casino Section */}
              <SidebarSection title="Casino">
                <SidebarLink href="/casino" icon="🎰" label="All Games" active={pathname === "/casino"} />
                <SidebarLink href="/casino/live_casino" icon="🔴" label="Live Casino" active={pathname === "/casino/live_casino"} />
                <SidebarLink href="/casino/slots" icon="🎲" label="Slots" active={pathname === "/casino/slots"} />
                <SidebarLink href="/casino/crash_games" icon="🚀" label="Crash Games" active={pathname === "/casino/crash_games"} />
                <SidebarLink href="/casino/virtual_sports" icon="🎮" label="Virtual Sports" active={pathname === "/casino/virtual_sports"} />
              </SidebarSection>

              {/* Quick Links */}
              <SidebarSection title="Quick Links">
                <SidebarLink href="/bets" icon="📋" label="My Bets" active={pathname === "/bets"} />
                <SidebarLink href="/wallet" icon="💰" label="Wallet" active={pathname === "/wallet"} />
                <SidebarLink href="/account" icon="👤" label="Account" active={pathname?.startsWith("/account") || false} />
                <SidebarLink href="/account/referral" icon="🎁" label="Referral" active={pathname === "/account/referral"} />
              </SidebarSection>
            </div>
          </aside>
        )}

        {/* ===== Main Content Area ===== */}
        <main className="flex-1 min-w-0 pb-16 md:pb-8">
          {children}
        </main>
      </div>

      <Footer />

      <WhatsAppWidget />

      {/* ===== Global Bet Slip Drawer (all pages) ===== */}
      <GlobalBetSlip />

      {/* ===== Mobile Bottom Navigation ===== */}
      <MobileNav />
      </AgeGate>
      </ErrorBoundary>
      </BetSlipProvider>
      </ToastProvider>
    </AuthProvider>
  );
}

// ========== Sidebar Components ==========

function SidebarSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mb-1">
      <div className="px-4 py-1.5">
        <span className="text-[10px] font-semibold text-gray-500 uppercase tracking-wider">
          {title}
        </span>
      </div>
      <div>{children}</div>
    </div>
  );
}

function SidebarLink({
  href,
  icon,
  label,
  active,
}: {
  href: string;
  icon: string;
  label: string;
  active: boolean;
}) {
  return (
    <Link
      href={href}
      className={`flex items-center gap-2.5 px-4 py-2 text-[13px] transition-colors ${
        active
          ? "text-white bg-lotus/15 border-l-2 border-lotus font-medium"
          : "text-gray-400 hover:text-white hover:bg-white/5"
      }`}
    >
      <span className="text-sm w-5 text-center">{icon}</span>
      {label}
    </Link>
  );
}

// ========== Expandable Sport Tree ==========

function SportTree({
  sport,
  label,
  icon,
  pathname,
}: {
  sport: string;
  label: string;
  icon: string;
  pathname: string | null;
}) {
  const [expanded, setExpanded] = useState(false);
  const [competitions, setCompetitions] = useState<Competition[]>([]);
  const [loaded, setLoaded] = useState(false);

  const isActive = pathname?.includes(`/sports/${sport.replace(/_/g, "-")}`) || false;

  // Auto-expand if this sport is in the current path
  useEffect(() => {
    if (isActive && !expanded) {
      setExpanded(true);
    }
  }, [isActive]);

  // Load competitions when expanded
  useEffect(() => {
    if (expanded && !loaded) {
      api.fetchCompetitions(sport).then((data) => {
        const arr = Array.isArray(data) ? data : [];
        setCompetitions(arr);
        setLoaded(true);
      }).catch(() => {
        setLoaded(true);
      });
    }
  }, [expanded, loaded, sport]);

  const sportSlug = sport.replace(/_/g, "-");

  return (
    <div>
      {/* Sport row with expand/collapse */}
      <div className="flex items-center">
        <Link
          href={`/sports/${sportSlug}`}
          className={`flex-1 flex items-center gap-2.5 pl-4 pr-1 py-2 text-[13px] transition-colors ${
            isActive
              ? "text-white bg-lotus/15 border-l-2 border-lotus font-medium"
              : "text-gray-400 hover:text-white hover:bg-white/5"
          }`}
        >
          <span className="text-sm w-5 text-center">{icon}</span>
          {label}
        </Link>
        <button
          onClick={(e) => { e.preventDefault(); e.stopPropagation(); setExpanded(!expanded); }}
          className="px-3 py-2 text-gray-500 hover:text-white hover:bg-white/5 transition rounded"
          aria-label={expanded ? "Collapse" : "Expand"}
        >
          <svg
            className={`w-3.5 h-3.5 transition-transform duration-200 ${expanded ? "rotate-90" : ""}`}
            fill="none" stroke="currentColor" viewBox="0 0 24 24"
          >
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M9 5l7 7-7 7" />
          </svg>
        </button>
      </div>

      {/* Competitions (leagues) */}
      {expanded && (
        <div className="ml-4 border-l border-gray-800/40">
          {!loaded ? (
            <div className="pl-4 py-1.5">
              <div className="h-3 w-24 bg-surface-light rounded animate-pulse" />
            </div>
          ) : competitions.length === 0 ? (
            <div className="pl-4 py-1.5 text-[11px] text-gray-500">No leagues</div>
          ) : (
            competitions.map((comp) => (
              <Link
                key={comp.id}
                href={`/sports/${sportSlug}?competition=${comp.id}`}
                className="flex items-center justify-between pl-4 pr-3 py-1.5 text-[11px] text-gray-400 hover:text-white hover:bg-white/5 transition"
              >
                <span className="truncate">{comp.name}</span>
                <span className="text-[9px] text-gray-500 flex-shrink-0 ml-1">
                  {comp.match_count || 0}
                </span>
              </Link>
            ))
          )}
        </div>
      )}
    </div>
  );
}

// Bottom navigation is provided by <MobileNav/> — see components/MobileNav.tsx
