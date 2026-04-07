"use client";

import { useState, useEffect } from "react";

export default function AgeGate({ children }: { children: React.ReactNode }) {
  const [verified, setVerified] = useState<boolean | null>(null);

  useEffect(() => {
    const stored = localStorage.getItem("age_verified");
    if (stored) {
      try {
        const data = JSON.parse(stored);
        if (data && data.verified === true && data.timestamp) {
          setVerified(true);
          return;
        }
      } catch {
        // invalid data
      }
    }
    setVerified(false);
  }, []);

  function handleConfirm() {
    localStorage.setItem(
      "age_verified",
      JSON.stringify({ verified: true, timestamp: Date.now() })
    );
    setVerified(true);
  }

  function handleUnder18() {
    window.location.href = "https://www.google.com";
  }

  // Still loading
  if (verified === null) {
    return null;
  }

  // Verified, render children
  if (verified) {
    return <>{children}</>;
  }

  // Not verified, show modal
  return (
    <div className="fixed inset-0 z-[9999] bg-black/90 flex items-center justify-center p-4">
      <div className="bg-[var(--bg-surface)] border border-gray-700 rounded-2xl max-w-md w-full p-8 text-center shadow-2xl">
        {/* 18+ Badge */}
        <div className="mx-auto w-20 h-20 rounded-full bg-lotus/20 border-2 border-lotus flex items-center justify-center mb-6">
          <span className="text-3xl font-black text-lotus">18+</span>
        </div>

        <h2 className="text-xl font-bold text-white mb-2">Age Verification Required</h2>
        <p className="text-sm text-gray-400 mb-6">
          3XBet is an online betting platform. You must be at least 18 years of age to access
          this site. Please confirm your age to continue.
        </p>

        <div className="space-y-3">
          <button
            onClick={handleConfirm}
            className="w-full bg-lotus hover:bg-lotus-light text-white font-semibold py-3 rounded-xl transition text-sm"
          >
            I confirm I am 18 years or older
          </button>
          <button
            onClick={handleUnder18}
            className="w-full bg-surface-light hover:bg-surface-lighter text-gray-400 font-medium py-3 rounded-xl transition text-sm border border-gray-700"
          >
            I am under 18
          </button>
        </div>

        <p className="text-[10px] text-gray-400 mt-4 leading-relaxed">
          By clicking &quot;I confirm I am 18 years or older&quot;, you agree to our{" "}
          <span className="text-gray-500">Terms &amp; Conditions</span> and confirm that you are of
          legal gambling age in your jurisdiction.
        </p>
      </div>
    </div>
  );
}
