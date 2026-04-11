"use client";

import { useEffect } from "react";

interface ErrorProps {
  error: Error & { digest?: string };
  reset: () => void;
}

export default function RootError({ error, reset }: ErrorProps) {
  useEffect(() => {
    // Log to console; replace with Sentry/etc if wired up later.
    console.error("[root error boundary]", error);
  }, [error]);

  return (
    <div className="min-h-[60vh] flex items-center justify-center px-4">
      <div className="w-full max-w-md text-center bg-surface border border-gray-800/60 rounded-xl p-8 shadow-2xl">
        <div className="text-5xl mb-4" aria-hidden="true">&#x26A0;&#xFE0F;</div>
        <h1 className="text-xl font-bold text-white mb-2">
          Something went wrong
        </h1>
        <p className="text-sm text-gray-400 mb-4">
          We hit an unexpected error. The team has been notified.
        </p>
        {error?.message && (
          <p className="text-xs text-gray-500 mb-6 font-mono break-words bg-black/30 border border-gray-800/60 rounded px-3 py-2">
            {error.message}
          </p>
        )}
        <div className="flex items-center justify-center gap-2">
          <button
            type="button"
            onClick={() => reset()}
            className="bg-lotus hover:bg-lotus-light text-white px-4 py-2 rounded-lg text-sm font-semibold transition"
          >
            Try again
          </button>
          <a
            href="/"
            className="bg-white/5 hover:bg-white/10 text-gray-300 px-4 py-2 rounded-lg text-sm font-semibold transition"
          >
            Go home
          </a>
        </div>
      </div>
    </div>
  );
}
