"use client";

import { useState } from "react";
import { useAuth } from "@/lib/auth";
import { api } from "@/lib/api";
import Link from "next/link";
import Select from "@/components/Select";
import { useToast } from "@/components/Toast";

type Method = "upi" | "bank" | "crypto";

export default function WithdrawPage() {
  const { isLoggedIn, balance, refreshBalance } = useAuth();
  const { addToast } = useToast();
  const [method, setMethod] = useState<Method>("upi");
  const [amount, setAmount] = useState("");
  const [upiId, setUpiId] = useState("");
  const [bankAccount, setBankAccount] = useState("");
  const [bankIfsc, setBankIfsc] = useState("");
  const [cryptoAddress, setCryptoAddress] = useState("");
  const [cryptoCurrency, setCryptoCurrency] = useState("USDT");
  const [loading, setLoading] = useState(false);

  if (!isLoggedIn) {
    return (
      <div className="max-w-7xl mx-auto px-3 py-16 text-center">
        <h2 className="text-lg font-bold text-white">Please Login</h2>
        <Link href="/login" className="inline-block mt-4 bg-lotus text-white px-6 py-2 rounded-lg text-sm">Login</Link>
      </div>
    );
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const amt = parseFloat(amount);
    if (!amt || amt <= 0) { addToast({ type: "warning", title: "Enter a valid amount" }); return; }
    if (balance && amt > (balance.available_balance || 0)) { addToast({ type: "error", title: "Insufficient balance" }); return; }

    setLoading(true);
    try {
      const details: Record<string, string> = {};
      if (method === "upi") { details.upi_id = upiId; }
      else if (method === "bank") { details.bank_account = bankAccount; details.bank_ifsc = bankIfsc; }
      else { details.crypto_address = cryptoAddress; details.crypto_currency = cryptoCurrency; }

      await api.initiateWithdrawal(amt, method, details);
      addToast({ type: "success", title: `Withdrawal of ₹${amt.toLocaleString("en-IN")} initiated` });
      setAmount("");
      refreshBalance();
    } catch {
      addToast({ type: "error", title: "Withdrawal failed" });
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="max-w-lg mx-auto px-3 py-4 space-y-4">
      <div className="flex items-center gap-2 text-xs text-gray-500">
        <Link href="/account" className="hover:text-white transition">Account</Link>
        <span>/</span>
        <span className="text-white">Withdraw</span>
      </div>

      <h1 className="text-lg font-bold text-white">Withdraw Funds</h1>

      {/* Balance */}
      {balance && (
        <div className="bg-surface rounded-lg border border-gray-800/60 p-3 flex justify-between">
          <span className="text-xs text-gray-400">Available</span>
          <span className="text-sm font-bold text-profit font-mono">₹{(balance.available_balance || 0).toLocaleString("en-IN")}</span>
        </div>
      )}

      {/* Method tabs */}
      <div className="flex gap-1">
        {(["upi", "bank", "crypto"] as Method[]).map((m) => (
          <button key={m} onClick={() => setMethod(m)}
            className={`flex-1 py-2 rounded-lg text-xs font-medium transition ${method === m ? "bg-lotus text-white" : "bg-surface border border-gray-800/60 text-gray-400"}`}>
            {m === "upi" ? "UPI" : m === "bank" ? "Bank" : "Crypto"}
          </button>
        ))}
      </div>

      {/* Form */}
      <form onSubmit={handleSubmit} className="bg-surface rounded-lg border border-gray-800/60 p-4 space-y-3">
        <div>
          <label className="text-xs text-gray-400">Amount (₹)</label>
          <input type="number" value={amount} onChange={(e) => setAmount(e.target.value)}
            placeholder="Min ₹500" min="500" required
            className="w-full mt-1 h-9 px-3 text-sm bg-[var(--bg-primary)] border border-gray-700/60 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus" />
        </div>

        {method === "upi" && (
          <div>
            <label className="text-xs text-gray-400">UPI ID</label>
            <input type="text" value={upiId} onChange={(e) => setUpiId(e.target.value)}
              placeholder="name@upi" required
              className="w-full mt-1 h-9 px-3 text-sm bg-[var(--bg-primary)] border border-gray-700/60 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus" />
          </div>
        )}

        {method === "bank" && (
          <>
            <div>
              <label className="text-xs text-gray-400">Account Number</label>
              <input type="text" value={bankAccount} onChange={(e) => setBankAccount(e.target.value)}
                placeholder="Account number" required
                className="w-full mt-1 h-9 px-3 text-sm bg-[var(--bg-primary)] border border-gray-700/60 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus" />
            </div>
            <div>
              <label className="text-xs text-gray-400">IFSC Code</label>
              <input type="text" value={bankIfsc} onChange={(e) => setBankIfsc(e.target.value)}
                placeholder="IFSC code" required
                className="w-full mt-1 h-9 px-3 text-sm bg-[var(--bg-primary)] border border-gray-700/60 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus" />
            </div>
          </>
        )}

        {method === "crypto" && (
          <>
            <div>
              <label className="text-xs text-gray-400">Currency</label>
              <Select value={cryptoCurrency} onChange={setCryptoCurrency} className="mt-1"
                options={[{ value: "USDT", label: "USDT (TRC20)" }, { value: "BTC", label: "BTC" }, { value: "ETH", label: "ETH" }]} />
            </div>
            <div>
              <label className="text-xs text-gray-400">Wallet Address</label>
              <input type="text" value={cryptoAddress} onChange={(e) => setCryptoAddress(e.target.value)}
                placeholder="Wallet address" required
                className="w-full mt-1 h-9 px-3 text-sm bg-[var(--bg-primary)] border border-gray-700/60 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus" />
            </div>
          </>
        )}

        <button type="submit" disabled={loading}
          className="w-full h-10 bg-lotus hover:bg-lotus-light disabled:bg-gray-700 text-white text-sm font-semibold rounded-lg transition disabled:opacity-50">
          {loading ? "Processing..." : `Withdraw ₹${amount || "0"}`}
        </button>
      </form>
    </div>
  );
}
