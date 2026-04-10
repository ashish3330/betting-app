"use client";

/**
 * Fixed bottom navigation shown only on screens < 768px.
 * Five items: Home, Sports, Live, Wallet, Account — mirroring the structure
 * of reference betting exchanges (Betfair/Lotus365/playzone9).
 */

import Link from "next/link";
import { usePathname } from "next/navigation";
import { ReactNode } from "react";

interface NavItem {
  href: string;
  label: string;
  icon: ReactNode;
  match: (pathname: string) => boolean;
}

const NAV_ITEMS: NavItem[] = [
  {
    href: "/",
    label: "Home",
    match: (p) => p === "/",
    icon: (
      <svg
        className="w-5 h-5"
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
      >
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={1.8}
          d="M3 12l9-9 9 9M5 10v10a1 1 0 001 1h3m10-11v11a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6"
        />
      </svg>
    ),
  },
  {
    href: "/sports",
    label: "Sports",
    match: (p) => p.startsWith("/sports"),
    icon: (
      <svg
        className="w-5 h-5"
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
      >
        <circle cx="12" cy="12" r="9" strokeWidth={1.8} />
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={1.8}
          d="M3 12h18M12 3a14 14 0 010 18M12 3a14 14 0 000 18"
        />
      </svg>
    ),
  },
  {
    href: "/markets?filter=live",
    label: "Live",
    match: (p) => p.startsWith("/markets"),
    icon: (
      <span className="relative flex items-center justify-center w-5 h-5">
        <span className="absolute inline-flex h-3 w-3 rounded-full bg-red-500 opacity-75 animate-ping" />
        <span className="relative inline-flex rounded-full h-2.5 w-2.5 bg-red-500" />
      </span>
    ),
  },
  {
    href: "/wallet",
    label: "Wallet",
    match: (p) => p.startsWith("/wallet"),
    icon: (
      <svg
        className="w-5 h-5"
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
      >
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={1.8}
          d="M3 10a2 2 0 012-2h14a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2v-8zM3 10V8a2 2 0 012-2h12m4 6h-4a2 2 0 100 4h4"
        />
      </svg>
    ),
  },
  {
    href: "/account",
    label: "Account",
    match: (p) => p.startsWith("/account"),
    icon: (
      <svg
        className="w-5 h-5"
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
      >
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={1.8}
          d="M5.12 19.5A7.5 7.5 0 0112 16.5a7.5 7.5 0 016.88 3M15 9a3 3 0 11-6 0 3 3 0 016 0z"
        />
      </svg>
    ),
  },
];

export default function MobileNav() {
  const pathname = usePathname() || "/";
  // Hide nav on login/register flows so focus stays on the form
  if (
    pathname.startsWith("/login") ||
    pathname.startsWith("/register") ||
    pathname.startsWith("/casino/play")
  ) {
    return null;
  }

  return (
    <nav className="md:hidden fixed bottom-0 left-0 right-0 bg-[var(--nav-bg)] border-t border-gray-800 z-40">
      <div className="grid grid-cols-5 h-14">
        {NAV_ITEMS.map((item) => {
          const active = item.match(pathname);
          return (
            <Link
              key={item.href}
              href={item.href}
              className={`flex flex-col items-center justify-center gap-0.5 transition ${
                active ? "text-lotus" : "text-gray-500 active:text-lotus"
              }`}
              aria-current={active ? "page" : undefined}
            >
              {item.icon}
              <span className="text-[10px]">{item.label}</span>
            </Link>
          );
        })}
      </div>
    </nav>
  );
}
