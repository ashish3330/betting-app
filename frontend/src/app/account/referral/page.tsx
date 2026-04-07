"use client";
import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";

interface ReferralStats {
  referral_code: string;
  referral_link: string;
  total_referrals: number;
  total_earnings: number;
  referred_users: ReferredUser[];
}

interface ReferredUser {
  username: string;
  joined_at: string;
  status: string;
  earnings: number;
}

export default function ReferralPage() {
  const { user } = useAuth();
  const [stats, setStats] = useState<ReferralStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [copied, setCopied] = useState(false);
  const [copiedLink, setCopiedLink] = useState(false);

  useEffect(() => {
    api
      .request<ReferralStats>("/api/v1/referral/stats", { auth: true })
      .then(setStats)
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const copyToClipboard = (text: string, type: "code" | "link") => {
    navigator.clipboard.writeText(text).then(() => {
      if (type === "code") {
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      } else {
        setCopiedLink(true);
        setTimeout(() => setCopiedLink(false), 2000);
      }
    });
  };

  const shareWhatsApp = () => {
    if (!stats) return;
    const message = encodeURIComponent(
      `Join 3XBet using my referral link and get bonus rewards!\n\n${stats.referral_link}\n\nReferral Code: ${stats.referral_code}`
    );
    window.open(`https://wa.me/?text=${message}`, "_blank");
  };

  if (loading) {
    return (
      <div className="p-4 md:p-6">
        <div className="max-w-2xl mx-auto">
          <div className="animate-pulse space-y-4">
            <div className="h-8 bg-gray-800 rounded w-48" />
            <div className="h-32 bg-gray-800 rounded" />
            <div className="h-48 bg-gray-800 rounded" />
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="p-4 md:p-6">
      <div className="max-w-2xl mx-auto space-y-6">
        <h1 className="text-xl font-bold text-white">Referral Program</h1>
        <p className="text-sm text-gray-400">
          Invite friends and earn 1% commission on their first deposit.
        </p>

        {/* Referral Code */}
        <div className="bg-[var(--bg-surface)] border border-gray-800 rounded-lg p-4 space-y-3">
          <h2 className="text-sm font-semibold text-gray-300">
            Your Referral Code
          </h2>
          <div className="flex items-center gap-2">
            <div className="flex-1 bg-black/40 border border-gray-700 rounded px-3 py-2 font-mono text-yellow-400 text-lg tracking-wider">
              {stats?.referral_code || "---"}
            </div>
            <button
              onClick={() =>
                stats && copyToClipboard(stats.referral_code, "code")
              }
              className="px-4 py-2 bg-gray-800 hover:bg-gray-700 text-white text-sm rounded transition-colors"
            >
              {copied ? "Copied!" : "Copy"}
            </button>
          </div>

          <h2 className="text-sm font-semibold text-gray-300 mt-4">
            Referral Link
          </h2>
          <div className="flex items-center gap-2">
            <div className="flex-1 bg-black/40 border border-gray-700 rounded px-3 py-2 text-xs text-gray-400 truncate">
              {stats?.referral_link || "---"}
            </div>
            <button
              onClick={() =>
                stats && copyToClipboard(stats.referral_link, "link")
              }
              className="px-4 py-2 bg-gray-800 hover:bg-gray-700 text-white text-sm rounded transition-colors"
            >
              {copiedLink ? "Copied!" : "Copy"}
            </button>
          </div>

          {/* Share buttons */}
          <div className="flex gap-2 mt-3">
            <button
              onClick={shareWhatsApp}
              className="flex items-center gap-2 px-4 py-2 bg-green-600 hover:bg-green-700 text-white text-sm rounded transition-colors"
            >
              <svg
                className="w-4 h-4"
                viewBox="0 0 24 24"
                fill="currentColor"
              >
                <path d="M17.472 14.382c-.297-.149-1.758-.867-2.03-.967-.273-.099-.471-.148-.67.15-.197.297-.767.966-.94 1.164-.173.199-.347.223-.644.075-.297-.15-1.255-.463-2.39-1.475-.883-.788-1.48-1.761-1.653-2.059-.173-.297-.018-.458.13-.606.134-.133.298-.347.446-.52.149-.174.198-.298.298-.497.099-.198.05-.371-.025-.52-.075-.149-.669-1.612-.916-2.207-.242-.579-.487-.5-.669-.51-.173-.008-.371-.01-.57-.01-.198 0-.52.074-.792.372-.272.297-1.04 1.016-1.04 2.479 0 1.462 1.065 2.875 1.213 3.074.149.198 2.096 3.2 5.077 4.487.709.306 1.262.489 1.694.625.712.227 1.36.195 1.871.118.571-.085 1.758-.719 2.006-1.413.248-.694.248-1.289.173-1.413-.074-.124-.272-.198-.57-.347m-5.421 7.403h-.004a9.87 9.87 0 01-5.031-1.378l-.361-.214-3.741.982.998-3.648-.235-.374a9.86 9.86 0 01-1.51-5.26c.001-5.45 4.436-9.884 9.888-9.884 2.64 0 5.122 1.03 6.988 2.898a9.825 9.825 0 012.893 6.994c-.003 5.45-4.437 9.884-9.885 9.884m8.413-18.297A11.815 11.815 0 0012.05 0C5.495 0 .16 5.335.157 11.892c0 2.096.547 4.142 1.588 5.945L.057 24l6.305-1.654a11.882 11.882 0 005.683 1.448h.005c6.554 0 11.89-5.335 11.893-11.893a11.821 11.821 0 00-3.48-8.413z" />
              </svg>
              Share via WhatsApp
            </button>
          </div>
        </div>

        {/* Stats */}
        <div className="grid grid-cols-2 gap-4">
          <div className="bg-[var(--bg-surface)] border border-gray-800 rounded-lg p-4 text-center">
            <p className="text-2xl font-bold text-white">
              {stats?.total_referrals || 0}
            </p>
            <p className="text-xs text-gray-500 mt-1">Total Referrals</p>
          </div>
          <div className="bg-[var(--bg-surface)] border border-gray-800 rounded-lg p-4 text-center">
            <p className="text-2xl font-bold text-green-400">
              {"\u20B9"}
              {(stats?.total_earnings || 0).toLocaleString()}
            </p>
            <p className="text-xs text-gray-500 mt-1">Total Earnings</p>
          </div>
        </div>

        {/* Referred Users */}
        <div className="bg-[var(--bg-surface)] border border-gray-800 rounded-lg overflow-hidden">
          <div className="px-4 py-3 border-b border-gray-800">
            <h2 className="text-sm font-semibold text-gray-300">
              Referred Users
            </h2>
          </div>
          {stats?.referred_users && stats.referred_users.length > 0 ? (
            <div className="divide-y divide-gray-800/50">
              {stats.referred_users.map((ru, i) => (
                <div
                  key={i}
                  className="px-4 py-3 flex items-center justify-between"
                >
                  <div>
                    <p className="text-sm text-white">{ru.username}</p>
                    <p className="text-xs text-gray-500">
                      Joined{" "}
                      {new Date(ru.joined_at).toLocaleDateString("en-IN")}
                    </p>
                  </div>
                  <div className="text-right">
                    <span
                      className={`text-xs px-2 py-0.5 rounded ${
                        ru.status === "active"
                          ? "bg-green-900/40 text-green-400"
                          : "bg-gray-800 text-gray-400"
                      }`}
                    >
                      {ru.status}
                    </span>
                    <p className="text-xs text-green-400 mt-1">
                      +{"\u20B9"}
                      {ru.earnings.toLocaleString()}
                    </p>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="px-4 py-8 text-center text-sm text-gray-500">
              No referrals yet. Share your code to start earning!
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
