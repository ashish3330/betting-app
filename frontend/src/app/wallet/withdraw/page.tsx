"use client";

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { useAuth } from "@/lib/auth";
import { api } from "@/lib/api";

export default function WithdrawPage() {
  const { isLoggedIn, isLoading } = useAuth();
  const router = useRouter();

  const [amount, setAmount] = useState("");
  const [upiId, setUpiId] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<{ transaction_id: string; status: string } | null>(null);

  useEffect(() => {
    if (!isLoading && !isLoggedIn) {
      router.push("/login");
    }
  }, [isLoading, isLoggedIn, router]);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setResult(null);
    const amt = parseFloat(amount);
    if (isNaN(amt) || amt <= 0) {
      setError("Please enter a valid amount");
      return;
    }
    if (!upiId || !upiId.includes("@")) {
      setError("Please enter a valid UPI ID");
      return;
    }
    setSubmitting(true);
    try {
      const res = await api.initiateWithdrawal(amt, "upi", { upi_id: upiId });
      setResult(res);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Withdrawal failed");
    } finally {
      setSubmitting(false);
    }
  }

  if (isLoading || !isLoggedIn) return null;

  return (
    <div className="max-w-md mx-auto px-3 py-6 space-y-4">
      <div>
        <h1 className="text-lg font-bold text-white">Withdraw Funds</h1>
        <p className="text-xs text-gray-500 mt-1">Withdraw money from your wallet to UPI</p>
      </div>

      <div className="bg-surface rounded-xl border border-gray-800 p-5">
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="text-xs text-gray-400 block mb-1">Amount (₹)</label>
            <input
              type="number"
              min="1"
              step="1"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              placeholder="Enter amount"
              className="w-full h-10 px-3 bg-surface-light border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-lotus"
            />
          </div>

          <div>
            <label className="text-xs text-gray-400 block mb-1">UPI ID</label>
            <input
              type="text"
              value={upiId}
              onChange={(e) => setUpiId(e.target.value)}
              placeholder="yourname@upi"
              className="w-full h-10 px-3 bg-surface-light border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-lotus"
            />
          </div>

          {error && (
            <div className="text-xs text-loss bg-loss/10 border border-loss/20 rounded-lg px-3 py-2">
              {error}
            </div>
          )}
          {result && (
            <div className="text-xs text-profit bg-profit/10 border border-profit/20 rounded-lg px-3 py-2 space-y-1">
              <div>Withdrawal initiated. Transaction ID: {result.transaction_id}</div>
              <div>Status: {result.status}</div>
            </div>
          )}

          <button
            type="submit"
            disabled={submitting}
            className="w-full h-10 bg-lotus hover:bg-lotus-light text-white rounded-lg text-sm font-semibold transition disabled:opacity-50"
          >
            {submitting ? "Processing..." : "Initiate Withdrawal"}
          </button>
        </form>
      </div>

      <Link href="/wallet" className="block text-center text-xs text-lotus hover:underline">
        Back to Wallet
      </Link>
    </div>
  );
}
