"use client";

import Link from "next/link";

export default function Footer() {
  return (
    <footer className="hidden md:block bg-[var(--bg-surface)] border-t border-gray-800/60 mt-8">
      <div className="max-w-7xl mx-auto px-6 py-8">
        {/* Links Row */}
        <div className="flex flex-wrap items-center justify-center gap-6 mb-6">
          <Link href="/terms" className="text-xs text-gray-500 hover:text-white transition">
            Terms &amp; Conditions
          </Link>
          <Link href="/privacy" className="text-xs text-gray-500 hover:text-white transition">
            Privacy Policy
          </Link>
          <Link href="/responsible-gambling" className="text-xs text-gray-500 hover:text-white transition">
            Responsible Gambling
          </Link>
          <Link href="mailto:support@lotusexchange.com" className="text-xs text-gray-500 hover:text-white transition">
            Contact
          </Link>
        </div>

        {/* 18+ Badge + Payment Methods */}
        <div className="flex items-center justify-center gap-6 mb-6">
          {/* 18+ Badge */}
          <div className="w-10 h-10 rounded-full bg-loss/20 border border-loss/60 flex items-center justify-center">
            <span className="text-xs font-black text-loss">18+</span>
          </div>

          {/* Payment Methods */}
          <div className="flex items-center gap-3">
            <span className="text-[10px] text-gray-400 uppercase tracking-wider">Payments:</span>
            <div className="flex items-center gap-2">
              <span className="bg-surface-light border border-gray-700 rounded px-2 py-0.5 text-[10px] text-gray-400 font-medium">
                UPI
              </span>
              <span className="bg-surface-light border border-gray-700 rounded px-2 py-0.5 text-[10px] text-gray-400 font-medium">
                Crypto
              </span>
              <span className="bg-surface-light border border-gray-700 rounded px-2 py-0.5 text-[10px] text-gray-400 font-medium">
                Bank
              </span>
            </div>
          </div>
        </div>

        {/* Disclaimer */}
        <p className="text-[10px] text-gray-400 text-center leading-relaxed max-w-2xl mx-auto mb-4">
          Gambling involves risk. Please gamble responsibly. Only stake what you can afford to lose.
          If you feel you have a gambling problem, please visit our{" "}
          <Link href="/responsible-gambling" className="text-gray-500 hover:text-white underline">
            Responsible Gambling
          </Link>{" "}
          page for help and support.
        </p>

        {/* Copyright */}
        <div className="text-center">
          <p className="text-[10px] text-gray-500">
            &copy; 2026 Lotus Exchange. All rights reserved.
          </p>
        </div>
      </div>
    </footer>
  );
}
