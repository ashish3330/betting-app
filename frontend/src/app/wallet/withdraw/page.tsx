"use client";

import { useState, useEffect, useMemo } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { useAuth } from "@/lib/auth";
import {
  api,
  WalletBalance,
  WithdrawalDetails,
  WithdrawalResponse,
} from "@/lib/api";
import { useToast } from "@/components/Toast";

type Method = "upi" | "bank" | "crypto";
type Crypto = "USDT" | "BTC" | "ETH";

const PRESET_AMOUNTS = [500, 1000, 5000, 10000];
const MIN_WITHDRAW = 100;

function formatMoney(value: number | undefined | null) {
  const n = typeof value === "number" && isFinite(value) ? value : 0;
  return (
    "\u20B9" +
    n.toLocaleString("en-IN", {
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    })
  );
}

export default function WithdrawPage() {
  const { isLoggedIn, isLoading, user, refreshBalance } = useAuth();
  const router = useRouter();
  const { addToast } = useToast();

  const [method, setMethod] = useState<Method>("upi");
  const [balance, setBalance] = useState<WalletBalance | null>(null);
  const [amount, setAmount] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [result, setResult] = useState<WithdrawalResponse | null>(null);

  // Method-specific fields
  const [upiId, setUpiId] = useState("");
  const [bankAccount, setBankAccount] = useState("");
  const [bankIfsc, setBankIfsc] = useState("");
  const [bankName, setBankName] = useState("");
  const [cryptoAddress, setCryptoAddress] = useState("");
  const [cryptoCurrency, setCryptoCurrency] = useState<Crypto>("USDT");

  useEffect(() => {
    if (!isLoading && !isLoggedIn) {
      router.push("/login");
      return;
    }
    if (isLoggedIn) {
      api
        .getBalance()
        .then(setBalance)
        .catch(() => {});
    }
  }, [isLoading, isLoggedIn, router]);

  const available = balance?.available_balance ?? 0;

  const kycStatus = user?.kyc_status ?? "not_submitted";
  const kycVerified = kycStatus === "verified";

  const amountNumber = useMemo(() => {
    const n = parseFloat(amount);
    return isNaN(n) ? 0 : n;
  }, [amount]);

  const amountError = useMemo(() => {
    if (!amount) return "";
    if (amountNumber <= 0) return "Enter a valid amount";
    if (amountNumber < MIN_WITHDRAW)
      return `Minimum withdrawal is ${formatMoney(MIN_WITHDRAW)}`;
    if (amountNumber > available)
      return `Amount exceeds available balance (${formatMoney(available)})`;
    return "";
  }, [amount, amountNumber, available]);

  function handlePreset(v: number) {
    if (v <= available) {
      setAmount(String(v));
    } else {
      addToast({
        type: "warning",
        title: "Insufficient balance",
        message: `You only have ${formatMoney(available)} available.`,
      });
    }
  }

  function handleWithdrawAll() {
    const floored = Math.floor(available);
    if (floored < MIN_WITHDRAW) {
      addToast({
        type: "warning",
        title: "Balance too low",
        message: `Minimum withdrawal is ${formatMoney(MIN_WITHDRAW)}`,
      });
      return;
    }
    setAmount(String(floored));
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();

    if (!kycVerified) {
      addToast({
        type: "warning",
        title: "KYC required",
        message: "Please complete KYC verification before withdrawing.",
      });
      return;
    }

    if (amountError) {
      addToast({ type: "warning", title: amountError });
      return;
    }
    if (amountNumber <= 0) {
      addToast({ type: "warning", title: "Enter a valid amount" });
      return;
    }

    let details: WithdrawalDetails = {};
    if (method === "upi") {
      if (!upiId || !upiId.includes("@")) {
        addToast({ type: "warning", title: "Enter a valid UPI ID" });
        return;
      }
      details = { upi_id: upiId };
    } else if (method === "bank") {
      if (!bankAccount.trim() || bankAccount.length < 6) {
        addToast({ type: "warning", title: "Enter a valid account number" });
        return;
      }
      if (!bankIfsc.trim() || bankIfsc.length < 8) {
        addToast({ type: "warning", title: "Enter a valid IFSC code" });
        return;
      }
      details = {
        bank_account: bankAccount.trim(),
        bank_ifsc: bankIfsc.trim().toUpperCase(),
        bank_name: bankName.trim(),
      };
    } else if (method === "crypto") {
      if (!cryptoAddress.trim() || cryptoAddress.length < 20) {
        addToast({ type: "warning", title: "Enter a valid wallet address" });
        return;
      }
      details = {
        crypto_address: cryptoAddress.trim(),
        crypto_currency: cryptoCurrency,
      };
    }

    setSubmitting(true);
    try {
      const res = await api.initiateWithdrawal(amountNumber, method, details);
      setResult(res);
      addToast({
        type: "success",
        title: "Withdrawal requested",
        message: `Reference: ${res.transaction_id}`,
      });
      refreshBalance();
      // Refresh local balance
      api
        .getBalance()
        .then(setBalance)
        .catch(() => {});
    } catch (err) {
      addToast({
        type: "error",
        title: "Withdrawal failed",
        message: err instanceof Error ? err.message : "Please try again",
      });
    } finally {
      setSubmitting(false);
    }
  }

  if (isLoading || !isLoggedIn) return null;

  const METHOD_TABS: { key: Method; label: string }[] = [
    { key: "upi", label: "UPI" },
    { key: "bank", label: "Bank" },
    { key: "crypto", label: "Crypto" },
  ];

  return (
    <div className="max-w-xl mx-auto px-3 py-5 space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-bold text-white">Withdraw Funds</h1>
          <p className="text-[11px] text-gray-500 mt-0.5">
            Cash out to your preferred payout method
          </p>
        </div>
        <Link href="/wallet" className="text-[11px] text-gray-400 hover:text-white transition">
          Back
        </Link>
      </div>

      {/* Available balance card */}
      <div className="rounded-2xl border border-profit/30 bg-gradient-to-br from-profit/10 via-surface to-surface p-5">
        <div className="text-[10px] uppercase tracking-widest text-profit/80 font-semibold">
          Available to withdraw
        </div>
        <div className="mt-1 text-3xl font-bold font-mono tabular-nums text-profit">
          {formatMoney(available)}
        </div>
        <div className="text-[11px] text-gray-400 mt-1">
          Balance {formatMoney(balance?.balance ?? 0)} &middot; Exposure {formatMoney(balance?.exposure ?? 0)}
        </div>
      </div>

      {/* KYC warning */}
      {!kycVerified && (
        <div className="rounded-xl border border-yellow-500/30 bg-yellow-500/5 p-3 flex items-start gap-3">
          <div className="w-8 h-8 rounded-full bg-yellow-500/15 text-yellow-400 flex items-center justify-center flex-shrink-0">
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01M4.93 19h14.14a2 2 0 001.73-3l-7.07-12a2 2 0 00-3.46 0L3.2 16a2 2 0 001.73 3z" />
            </svg>
          </div>
          <div className="flex-1">
            <div className="text-xs font-semibold text-yellow-300">
              KYC verification required for withdrawals
            </div>
            <div className="text-[11px] text-yellow-200/70 mt-0.5">
              Complete your identity verification to unlock withdrawals.
            </div>
          </div>
          <Link
            href="/account"
            className="text-[11px] font-bold text-yellow-300 hover:text-yellow-200 underline whitespace-nowrap"
          >
            Go to KYC
          </Link>
        </div>
      )}

      {result ? (
        <div className="bg-surface rounded-xl border border-gray-800 p-5 space-y-3">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-full bg-profit/15 text-profit flex items-center justify-center">
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
              </svg>
            </div>
            <div>
              <div className="text-sm font-bold text-white">Withdrawal requested</div>
              <div className="text-[11px] text-gray-400">Status: {result.status}</div>
            </div>
          </div>
          <div className="rounded-lg border border-gray-800 bg-surface-light px-3 py-2 text-[11px]">
            <div className="text-gray-400">Transaction ID</div>
            <div className="text-white font-mono break-all">{result.transaction_id}</div>
          </div>
          {result.estimated_time && (
            <div className="text-[11px] text-gray-400">
              Estimated processing time: {result.estimated_time}
            </div>
          )}
          <div className="flex gap-2">
            <button
              onClick={() => {
                setResult(null);
                setAmount("");
              }}
              className="flex-1 h-11 bg-surface-lighter hover:bg-surface-light border border-gray-700 text-white rounded-lg text-sm font-bold transition"
            >
              New withdrawal
            </button>
            <button
              onClick={() => router.push("/wallet")}
              className="flex-1 h-11 bg-lotus hover:bg-lotus-light text-white rounded-lg text-sm font-bold transition"
            >
              View wallet
            </button>
          </div>
        </div>
      ) : (
        <div className="bg-surface rounded-xl border border-gray-800 p-5 space-y-4">
          {/* Method tabs */}
          <div className="grid grid-cols-3 gap-1 p-1 bg-surface-light rounded-lg border border-gray-800">
            {METHOD_TABS.map((tab) => (
              <button
                key={tab.key}
                type="button"
                onClick={() => setMethod(tab.key)}
                className={`h-9 rounded-md text-xs font-semibold transition ${
                  method === tab.key
                    ? "bg-lotus text-white shadow-md shadow-lotus/20"
                    : "text-gray-400 hover:text-white"
                }`}
              >
                {tab.label}
              </button>
            ))}
          </div>

          <form onSubmit={handleSubmit} className="space-y-4">
            {/* Preset chips */}
            <div>
              <div className="text-[11px] text-gray-400 mb-2">Quick amount</div>
              <div className="grid grid-cols-5 gap-2">
                {PRESET_AMOUNTS.map((p) => (
                  <button
                    key={p}
                    type="button"
                    onClick={() => handlePreset(p)}
                    disabled={p > available}
                    className={`h-9 rounded-lg text-[11px] font-semibold transition border ${
                      amount === String(p)
                        ? "border-lotus bg-lotus/10 text-lotus"
                        : "border-gray-700 bg-surface-lighter text-gray-300 hover:border-gray-500"
                    } disabled:opacity-40 disabled:cursor-not-allowed`}
                  >
                    {"\u20B9"}
                    {p.toLocaleString("en-IN")}
                  </button>
                ))}
                <button
                  type="button"
                  onClick={handleWithdrawAll}
                  className="h-9 rounded-lg text-[11px] font-semibold border border-lotus/40 bg-lotus/10 text-lotus hover:bg-lotus/20 transition"
                >
                  All
                </button>
              </div>
            </div>

            {/* Amount input */}
            <div>
              <label className="text-xs text-gray-400 block mb-1">Amount</label>
              <div className="relative">
                <span className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 text-sm">
                  {"\u20B9"}
                </span>
                <input
                  type="number"
                  min="1"
                  step="1"
                  value={amount}
                  onChange={(e) => setAmount(e.target.value)}
                  placeholder="Enter amount"
                  className={`w-full h-11 pl-7 pr-3 bg-surface-light border rounded-lg text-sm text-white focus:outline-none font-mono tabular-nums ${
                    amountError ? "border-loss focus:border-loss" : "border-gray-700 focus:border-lotus"
                  }`}
                />
              </div>
              {amountError ? (
                <div className="text-[10px] text-loss mt-1">{amountError}</div>
              ) : (
                <div className="text-[10px] text-gray-500 mt-1">
                  Minimum: {formatMoney(MIN_WITHDRAW)} &middot; Available: {formatMoney(available)}
                </div>
              )}
            </div>

            {/* Method-specific fields */}
            {method === "upi" && (
              <div>
                <label className="text-xs text-gray-400 block mb-1">UPI ID</label>
                <input
                  type="text"
                  value={upiId}
                  onChange={(e) => setUpiId(e.target.value)}
                  placeholder="yourname@okaxis"
                  className="w-full h-11 px-3 bg-surface-light border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-lotus"
                />
              </div>
            )}

            {method === "bank" && (
              <div className="space-y-3">
                <div>
                  <label className="text-xs text-gray-400 block mb-1">Account number</label>
                  <input
                    type="text"
                    value={bankAccount}
                    onChange={(e) => setBankAccount(e.target.value.replace(/\D/g, ""))}
                    placeholder="e.g. 50100123456789"
                    className="w-full h-11 px-3 bg-surface-light border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-lotus font-mono"
                  />
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="text-xs text-gray-400 block mb-1">IFSC</label>
                    <input
                      type="text"
                      value={bankIfsc}
                      onChange={(e) => setBankIfsc(e.target.value.toUpperCase())}
                      placeholder="HDFC0001234"
                      className="w-full h-11 px-3 bg-surface-light border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-lotus font-mono uppercase"
                    />
                  </div>
                  <div>
                    <label className="text-xs text-gray-400 block mb-1">Bank name</label>
                    <input
                      type="text"
                      value={bankName}
                      onChange={(e) => setBankName(e.target.value)}
                      placeholder="HDFC Bank"
                      className="w-full h-11 px-3 bg-surface-light border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-lotus"
                    />
                  </div>
                </div>
              </div>
            )}

            {method === "crypto" && (
              <div className="space-y-3">
                <div>
                  <label className="text-xs text-gray-400 block mb-1">Currency</label>
                  <select
                    value={cryptoCurrency}
                    onChange={(e) => setCryptoCurrency(e.target.value as Crypto)}
                    className="w-full h-11 px-3 bg-surface-light border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-lotus"
                  >
                    <option value="USDT">USDT (Tether)</option>
                    <option value="BTC">BTC (Bitcoin)</option>
                    <option value="ETH">ETH (Ethereum)</option>
                  </select>
                </div>
                <div>
                  <label className="text-xs text-gray-400 block mb-1">Wallet address</label>
                  <input
                    type="text"
                    value={cryptoAddress}
                    onChange={(e) => setCryptoAddress(e.target.value)}
                    placeholder="Paste your wallet address"
                    className="w-full h-11 px-3 bg-surface-light border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-lotus font-mono"
                  />
                  <div className="text-[10px] text-gray-500 mt-1">
                    Double-check the address. Crypto transfers are irreversible.
                  </div>
                </div>
              </div>
            )}

            {/* Fees disclosure */}
            <div className="rounded-lg border border-gray-800 bg-surface-light/50 px-3 py-2 text-[11px] text-gray-400 flex items-center justify-between">
              <span>Processing fee</span>
              <span className="text-white font-semibold">0% &middot; 1-24 hours</span>
            </div>

            <button
              type="submit"
              disabled={submitting || !kycVerified || !!amountError || amountNumber <= 0}
              className="w-full h-11 bg-lotus hover:bg-lotus-light text-white rounded-lg text-sm font-bold transition disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {submitting
                ? "Processing..."
                : `Withdraw ${amount ? formatMoney(amountNumber) : ""}`}
            </button>
          </form>
        </div>
      )}

      <Link href="/wallet" className="block text-center text-xs text-lotus hover:underline">
        View withdrawal history
      </Link>
    </div>
  );
}
