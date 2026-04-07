"use client";

import { useState, useEffect } from "react";
import Link from "next/link";

export default function DisclaimerBanner() {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    const dismissed = localStorage.getItem("disclaimer_dismissed");
    if (dismissed) {
      try {
        const data = JSON.parse(dismissed);
        // Dismiss lasts 24 hours
        if (data.timestamp && Date.now() - data.timestamp < 24 * 60 * 60 * 1000) {
          setVisible(false);
          return;
        }
      } catch {
        // invalid
      }
    }
    setVisible(true);
  }, []);

  function handleDismiss() {
    localStorage.setItem(
      "disclaimer_dismissed",
      JSON.stringify({ timestamp: Date.now() })
    );
    setVisible(false);
  }

  if (!visible) return null;

  return (
    <div className="bg-amber-600/90 text-black text-xs font-medium flex items-center justify-center gap-3 px-3 py-1.5 relative z-[60]">
      <span className="font-bold">18+</span>
      <span className="hidden sm:inline">|</span>
      <span className="hidden sm:inline">Gamble Responsibly</span>
      <span>|</span>
      <Link href="/terms" className="underline hover:no-underline">
        T&amp;C Apply
      </Link>
      <span className="hidden sm:inline">|</span>
      <Link href="/responsible-gambling" className="underline hover:no-underline hidden sm:inline">
        Responsible Gambling
      </Link>
      <button
        onClick={handleDismiss}
        className="absolute right-2 top-1/2 -translate-y-1/2 text-black/60 hover:text-black transition"
        aria-label="Dismiss disclaimer"
      >
        <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
        </svg>
      </button>
    </div>
  );
}
