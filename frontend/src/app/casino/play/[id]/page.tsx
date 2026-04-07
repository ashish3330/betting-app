"use client";
import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";

interface GameStream {
  game_id: string;
  name: string;
  stream_url: string;
  iframe_url: string;
  provider: string;
  active: boolean;
  token: string;
  expires_at: string;
}

export default function PlayGamePage() {
  const { id } = useParams();
  const router = useRouter();
  const { balance } = useAuth();
  const [stream, setStream] = useState<GameStream | null>(null);
  const [elapsed, setElapsed] = useState(0);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .request<GameStream>(`/api/v1/casino/game/${id}/stream`)
      .then(setStream)
      .catch((err) => setError(err.message || "Failed to load game"));

    const timer = setInterval(() => setElapsed((e) => e + 1), 1000);
    return () => clearInterval(timer);
  }, [id]);

  const mins = Math.floor(elapsed / 60);
  const secs = elapsed % 60;

  const formatGameName = (gameId: string): string => {
    return gameId
      .replace(/_/g, " ")
      .replace(/\b\w/g, (c) => c.toUpperCase());
  };

  if (error) {
    return (
      <div className="fixed inset-0 bg-black z-50 flex items-center justify-center dark-section">
        <div className="text-center">
          <p className="text-red-400 mb-4">{error}</p>
          <button
            onClick={() => router.back()}
            className="px-4 py-2 bg-lotus text-white rounded text-sm"
          >
            Go Back
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="fixed inset-0 bg-black z-50 flex flex-col">
      {/* Top bar */}
      <div className="h-10 bg-[var(--bg-surface)] flex items-center justify-between px-3 flex-shrink-0 border-b border-gray-800/40">
        <div className="flex items-center gap-3">
          <button
            onClick={() => router.back()}
            className="text-xs text-gray-400 hover:text-white transition-colors"
          >
            &larr; Exit
          </button>
          <span className="text-xs text-gray-500">
            {stream?.provider || "Loading..."}
          </span>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-xs text-gray-500">
            {mins}:{secs.toString().padStart(2, "0")}
          </span>
          <span className="text-xs text-green-400 font-mono">
            {"\u20B9"}
            {balance?.available_balance?.toLocaleString() || "0"}
          </span>
        </div>
      </div>

      {/* Game iframe */}
      <div className="flex-1 relative dark-section">
        {stream ? (
          <iframe
            src={stream.iframe_url}
            className="w-full h-full border-0"
            allow="autoplay; fullscreen"
            sandbox="allow-scripts allow-same-origin"
          />
        ) : (
          <div className="flex items-center justify-center h-full">
            <div className="text-center">
              <div className="w-12 h-12 border-2 border-yellow-500 border-t-transparent rounded-full animate-spin mx-auto mb-3" />
              <p className="text-sm text-gray-400">Loading game...</p>
            </div>
          </div>
        )}

        {/* Demo overlay for mock mode */}
        <div className="absolute inset-0 flex items-center justify-center bg-gradient-to-br from-purple-900/80 to-blue-900/80 pointer-events-none">
          <div className="text-center">
            <div className="text-6xl mb-4">
              <svg
                className="w-16 h-16 mx-auto text-yellow-400"
                fill="currentColor"
                viewBox="0 0 24 24"
              >
                <path d="M19 3H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm-7 14c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zm2-6h-4V5h4v6z" />
              </svg>
            </div>
            <h2 className="text-2xl font-bold text-white mb-2">
              {stream?.name ||
                (typeof id === "string" ? formatGameName(id) : "Game")}
            </h2>
            <p className="text-sm text-gray-300">
              Demo Mode &mdash; Connect real provider for live game
            </p>
            <p className="text-xs text-gray-500 mt-2">
              Provider: {stream?.provider || "Loading..."}
            </p>
          </div>
        </div>
      </div>

      {/* Quick bet buttons (bottom bar) */}
      <div className="h-14 bg-[var(--bg-surface)] flex items-center justify-between px-3 flex-shrink-0 border-t border-gray-800/40">
        <div className="flex items-center gap-2">
          <span className="text-xs text-gray-500">Quick Bet:</span>
          {[100, 500, 1000, 5000].map((amount) => (
            <button
              key={amount}
              className="px-3 py-1.5 bg-gray-800 hover:bg-gray-700 text-white text-xs rounded transition-colors"
            >
              {"\u20B9"}
              {amount.toLocaleString()}
            </button>
          ))}
        </div>
        <button
          onClick={() => router.back()}
          className="px-4 py-1.5 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors"
        >
          Exit Game
        </button>
      </div>
    </div>
  );
}
