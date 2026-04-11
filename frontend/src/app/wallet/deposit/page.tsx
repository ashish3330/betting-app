"use client";

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { useAuth } from "@/lib/auth";
import {
  api,
  WalletBalance,
  DepositResponse,
  CryptoDepositResponse,
} from "@/lib/api";
import { useToast } from "@/components/Toast";

type Method = "upi" | "bank" | "crypto";
type Crypto = "USDT" | "BTC" | "ETH";

const PRESET_AMOUNTS = [500, 1000, 2500, 5000, 10000, 25000];
const MIN_DEPOSIT = 100;

const BANK_DETAILS = {
  account_name: "Lotus Exchange Pvt Ltd",
  account_number: "50100123456789",
  ifsc: "HDFC0001234",
  bank_name: "HDFC Bank",
  branch: "Mumbai Fort",
};

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

export default function DepositPage() {
  const { isLoggedIn, isLoading, refreshBalance } = useAuth();
  const router = useRouter();
  const { addToast } = useToast();

  const [method, setMethod] = useState<Method>("upi");
  const [balance, setBalance] = useState<WalletBalance | null>(null);

  // UPI state
  const [amount, setAmount] = useState("");
  const [upiId, setUpiId] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [upiResult, setUpiResult] = useState<DepositResponse | null>(null);

  // Bank state
  const [bankProof, setBankProof] = useState("");
  const [bankAmount, setBankAmount] = useState("");
  const [bankSubmitting, setBankSubmitting] = useState(false);
  const [bankResult, setBankResult] = useState<DepositResponse | null>(null);

  // Crypto state
  const [cryptoAmount, setCryptoAmount] = useState("");
  const [cryptoCurrency, setCryptoCurrency] = useState<Crypto>("USDT");
  const [cryptoSubmitting, setCryptoSubmitting] = useState(false);
  const [cryptoResult, setCryptoResult] = useState<CryptoDepositResponse | null>(null);

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

  async function handleUpiSubmit(e: React.FormEvent) {
    e.preventDefault();
    const amt = parseFloat(amount);
    if (isNaN(amt) || amt <= 0) {
      addToast({ type: "warning", title: "Enter a valid amount" });
      return;
    }
    if (amt < MIN_DEPOSIT) {
      addToast({ type: "warning", title: `Minimum deposit is ${formatMoney(MIN_DEPOSIT)}` });
      return;
    }
    if (!upiId || !upiId.includes("@")) {
      addToast({ type: "warning", title: "Enter a valid UPI ID", message: "e.g. name@okaxis" });
      return;
    }
    setSubmitting(true);
    try {
      const res = await api.initiateUPIDeposit(amt, upiId);
      setUpiResult(res);
      addToast({
        type: "success",
        title: "Deposit initiated",
        message: `Reference: ${res.transaction_id}`,
      });
      refreshBalance();
    } catch (err) {
      addToast({
        type: "error",
        title: "Deposit failed",
        message: err instanceof Error ? err.message : "Please try again",
      });
    } finally {
      setSubmitting(false);
    }
  }

  async function handleBankSubmit(e: React.FormEvent) {
    e.preventDefault();
    const amt = parseFloat(bankAmount);
    if (isNaN(amt) || amt <= 0) {
      addToast({ type: "warning", title: "Enter the amount transferred" });
      return;
    }
    if (amt < MIN_DEPOSIT) {
      addToast({ type: "warning", title: `Minimum deposit is ${formatMoney(MIN_DEPOSIT)}` });
      return;
    }
    if (!bankProof.trim()) {
      addToast({ type: "warning", title: "Please paste the transaction reference or UTR" });
      return;
    }
    setBankSubmitting(true);
    try {
      // Bank proof is submitted through the same deposit endpoint for now;
      // the backend will route it to manual review.
      const res = await api.initiateUPIDeposit(amt, `bank-transfer:${bankProof.slice(0, 40)}`);
      setBankResult(res);
      addToast({
        type: "success",
        title: "Bank transfer submitted",
        message: "Our team will verify your deposit shortly.",
      });
      refreshBalance();
    } catch (err) {
      addToast({
        type: "error",
        title: "Submission failed",
        message: err instanceof Error ? err.message : "Please try again",
      });
    } finally {
      setBankSubmitting(false);
    }
  }

  async function handleCryptoSubmit(e: React.FormEvent) {
    e.preventDefault();
    const amt = parseFloat(cryptoAmount);
    if (isNaN(amt) || amt <= 0) {
      addToast({ type: "warning", title: "Enter a valid amount" });
      return;
    }
    if (amt < MIN_DEPOSIT) {
      addToast({ type: "warning", title: `Minimum deposit is ${formatMoney(MIN_DEPOSIT)}` });
      return;
    }
    setCryptoSubmitting(true);
    try {
      const res = await api.initiateCryptoDeposit(amt, cryptoCurrency);
      setCryptoResult(res);
      addToast({
        type: "success",
        title: "Crypto deposit initiated",
        message: `Send ${res.amount_crypto} ${res.currency}`,
      });
      refreshBalance();
    } catch (err) {
      addToast({
        type: "error",
        title: "Crypto deposit failed",
        message: err instanceof Error ? err.message : "Please try again",
      });
    } finally {
      setCryptoSubmitting(false);
    }
  }

  async function copyToClipboard(value: string, label: string) {
    try {
      await navigator.clipboard.writeText(value);
      addToast({ type: "success", title: `${label} copied` });
    } catch {
      addToast({ type: "error", title: "Copy failed" });
    }
  }

  if (isLoading || !isLoggedIn) return null;

  const METHOD_TABS: { key: Method; label: string; icon: React.ReactNode }[] = [
    {
      key: "upi",
      label: "UPI",
      icon: (
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
        </svg>
      ),
    },
    {
      key: "bank",
      label: "Bank Transfer",
      icon: (
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 10h18M5 6h14l-7-3-7 3zM4 10v9h16v-9M9 14v2m6-2v2" />
        </svg>
      ),
    },
    {
      key: "crypto",
      label: "Crypto",
      icon: (
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8c-2 0-4 1-4 3s2 3 4 3 4 1 4 3-2 3-4 3m0-12V6m0 14v-2m0-10a3 3 0 013 3m-3 3a3 3 0 00-3 3" />
        </svg>
      ),
    },
  ];

  return (
    <div className="max-w-xl mx-auto px-3 py-5 space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-bold text-white">Deposit Funds</h1>
          <p className="text-[11px] text-gray-500 mt-0.5">
            Choose a method to add money to your wallet
          </p>
        </div>
        <Link
          href="/wallet"
          className="text-[11px] text-gray-400 hover:text-white transition"
        >
          Back
        </Link>
      </div>

      {/* Live balance display */}
      <div className="rounded-xl border border-gray-800 bg-surface px-4 py-3 flex items-center justify-between">
        <div>
          <div className="text-[10px] uppercase tracking-wider text-gray-500">
            Current Balance
          </div>
          <div className="text-lg font-bold text-white font-mono tabular-nums">
            {formatMoney(balance?.balance ?? 0)}
          </div>
        </div>
        <div className="text-right">
          <div className="text-[10px] uppercase tracking-wider text-gray-500">
            Available
          </div>
          <div className="text-lg font-bold text-profit font-mono tabular-nums">
            {formatMoney(balance?.available_balance ?? 0)}
          </div>
        </div>
      </div>

      {/* Method tabs */}
      <div className="grid grid-cols-3 gap-1 p-1 bg-surface rounded-xl border border-gray-800">
        {METHOD_TABS.map((tab) => (
          <button
            key={tab.key}
            onClick={() => setMethod(tab.key)}
            className={`flex items-center justify-center gap-1.5 h-9 rounded-lg text-xs font-semibold transition ${
              method === tab.key
                ? "bg-lotus text-white shadow-md shadow-lotus/20"
                : "text-gray-400 hover:text-white"
            }`}
          >
            {tab.icon}
            <span className="hidden sm:inline">{tab.label}</span>
            <span className="sm:hidden">
              {tab.key === "upi" ? "UPI" : tab.key === "bank" ? "Bank" : "Crypto"}
            </span>
          </button>
        ))}
      </div>

      {/* UPI tab content */}
      {method === "upi" && (
        <div className="bg-surface rounded-xl border border-gray-800 p-5">
          {upiResult ? (
            <SuccessCard
              title="Deposit Initiated"
              transactionId={upiResult.transaction_id}
              status={upiResult.status}
              onDone={() => {
                setUpiResult(null);
                setAmount("");
                setUpiId("");
                router.push("/wallet");
              }}
            >
              {upiResult.qr_code && (
                <div className="my-3 flex flex-col items-center">
                  <img
                    src={upiResult.qr_code}
                    alt="UPI QR"
                    className="w-44 h-44 rounded-lg border border-gray-700 bg-white p-2"
                  />
                  <div className="text-[11px] text-gray-400 mt-2">
                    Scan this QR with any UPI app to complete payment
                  </div>
                </div>
              )}
              {upiResult.upi_link && (
                <a
                  href={upiResult.upi_link}
                  className="block text-center mt-2 text-xs text-lotus underline"
                >
                  Open UPI link in app
                </a>
              )}
              <div className="mt-3 text-[11px] text-gray-400 leading-relaxed">
                Funds will reflect in your wallet within a few minutes after
                successful payment. Keep this reference handy if you need
                support.
              </div>
            </SuccessCard>
          ) : (
            <form onSubmit={handleUpiSubmit} className="space-y-4">
              {/* Preset chips */}
              <div>
                <div className="text-[11px] text-gray-400 mb-2">Quick amount</div>
                <div className="grid grid-cols-4 gap-2">
                  {PRESET_AMOUNTS.map((p) => (
                    <button
                      key={p}
                      type="button"
                      onClick={() => setAmount(String(p))}
                      className={`h-9 rounded-lg text-[11px] font-semibold transition border ${
                        amount === String(p)
                          ? "border-lotus bg-lotus/10 text-lotus"
                          : "border-gray-700 bg-surface-lighter text-gray-300 hover:border-gray-500"
                      }`}
                    >
                      {"\u20B9"}
                      {p.toLocaleString("en-IN")}
                    </button>
                  ))}
                  <button
                    type="button"
                    onClick={() => setAmount("")}
                    className="h-9 rounded-lg text-[11px] font-semibold border border-gray-700 bg-surface-lighter text-gray-300 hover:border-gray-500 transition"
                  >
                    Custom
                  </button>
                </div>
              </div>

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
                    className="w-full h-11 pl-7 pr-3 bg-surface-light border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-lotus font-mono tabular-nums"
                  />
                </div>
                <div className="text-[10px] text-gray-500 mt-1">
                  Minimum: {formatMoney(MIN_DEPOSIT)}
                </div>
              </div>

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

              <button
                type="submit"
                disabled={submitting}
                className="w-full h-11 bg-lotus hover:bg-lotus-light text-white rounded-lg text-sm font-bold transition disabled:opacity-50"
              >
                {submitting ? "Processing..." : `Deposit ${amount ? formatMoney(parseFloat(amount) || 0) : ""}`}
              </button>
            </form>
          )}
        </div>
      )}

      {/* Bank tab content */}
      {method === "bank" && (
        <div className="bg-surface rounded-xl border border-gray-800 p-5 space-y-4">
          {bankResult ? (
            <SuccessCard
              title="Bank Transfer Submitted"
              transactionId={bankResult.transaction_id}
              status={bankResult.status}
              onDone={() => {
                setBankResult(null);
                setBankAmount("");
                setBankProof("");
                router.push("/wallet");
              }}
            >
              <div className="mt-3 text-[11px] text-gray-400 leading-relaxed">
                Our team will verify the transfer against your submitted UTR and
                credit your wallet within 1-4 working hours.
              </div>
            </SuccessCard>
          ) : (
            <>
              <div>
                <div className="text-xs font-semibold text-white mb-2">
                  Transfer to our bank account
                </div>
                <div className="rounded-lg border border-gray-800 bg-surface-light divide-y divide-gray-800/70 text-xs">
                  {[
                    { label: "Account Name", value: BANK_DETAILS.account_name },
                    { label: "Account Number", value: BANK_DETAILS.account_number, copy: true },
                    { label: "IFSC", value: BANK_DETAILS.ifsc, copy: true },
                    { label: "Bank Name", value: BANK_DETAILS.bank_name },
                    { label: "Branch", value: BANK_DETAILS.branch },
                  ].map((row) => (
                    <div
                      key={row.label}
                      className="flex items-center justify-between px-3 py-2.5"
                    >
                      <div className="text-gray-400">{row.label}</div>
                      <div className="flex items-center gap-2">
                        <div className="text-white font-mono">{row.value}</div>
                        {row.copy && (
                          <button
                            type="button"
                            onClick={() => copyToClipboard(row.value, row.label)}
                            className="text-[10px] text-lotus hover:text-lotus-light px-1.5 py-0.5 rounded border border-lotus/30 hover:border-lotus/50 transition"
                          >
                            Copy
                          </button>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              </div>

              <form onSubmit={handleBankSubmit} className="space-y-4">
                <div>
                  <label className="text-xs text-gray-400 block mb-1">
                    Amount transferred
                  </label>
                  <div className="relative">
                    <span className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 text-sm">
                      {"\u20B9"}
                    </span>
                    <input
                      type="number"
                      min="1"
                      step="1"
                      value={bankAmount}
                      onChange={(e) => setBankAmount(e.target.value)}
                      placeholder="Enter amount"
                      className="w-full h-11 pl-7 pr-3 bg-surface-light border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-lotus font-mono tabular-nums"
                    />
                  </div>
                  <div className="text-[10px] text-gray-500 mt-1">
                    Minimum: {formatMoney(MIN_DEPOSIT)}
                  </div>
                </div>

                <div>
                  <label className="text-xs text-gray-400 block mb-1">
                    Transfer reference / UTR
                  </label>
                  <textarea
                    value={bankProof}
                    onChange={(e) => setBankProof(e.target.value)}
                    placeholder="Paste your UTR / transaction reference from your bank"
                    rows={3}
                    className="w-full px-3 py-2 bg-surface-light border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-lotus resize-none"
                  />
                </div>

                <button
                  type="submit"
                  disabled={bankSubmitting}
                  className="w-full h-11 bg-lotus hover:bg-lotus-light text-white rounded-lg text-sm font-bold transition disabled:opacity-50"
                >
                  {bankSubmitting ? "Submitting..." : "Submit proof of transfer"}
                </button>
              </form>
            </>
          )}
        </div>
      )}

      {/* Crypto tab content */}
      {method === "crypto" && (
        <div className="bg-surface rounded-xl border border-gray-800 p-5">
          {cryptoResult ? (
            <SuccessCard
              title="Crypto Deposit Initiated"
              transactionId={cryptoResult.transaction_id}
              status={cryptoResult.status}
              onDone={() => {
                setCryptoResult(null);
                setCryptoAmount("");
                router.push("/wallet");
              }}
            >
              <div className="mt-3 rounded-lg border border-gray-800 bg-surface-light p-3 space-y-2 text-xs">
                <div className="flex justify-between">
                  <span className="text-gray-400">Currency</span>
                  <span className="text-white font-semibold">
                    {cryptoResult.currency}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-gray-400">Amount to send</span>
                  <span className="text-white font-mono tabular-nums">
                    {cryptoResult.amount_crypto} {cryptoResult.currency}
                  </span>
                </div>
                <div>
                  <div className="text-gray-400 mb-1">Wallet address</div>
                  <div className="flex items-center gap-2">
                    <div className="flex-1 text-[11px] text-white font-mono break-all bg-surface rounded px-2 py-1.5 border border-gray-700">
                      {cryptoResult.wallet_address}
                    </div>
                    <button
                      type="button"
                      onClick={() =>
                        copyToClipboard(cryptoResult.wallet_address, "Wallet address")
                      }
                      className="text-[10px] text-lotus hover:text-lotus-light px-2 py-1.5 rounded border border-lotus/30 hover:border-lotus/50 transition"
                    >
                      Copy
                    </button>
                  </div>
                </div>
              </div>
              <div className="mt-3 text-[11px] text-gray-400 leading-relaxed">
                Send exactly the amount shown above to the wallet address. Funds
                are credited after network confirmations.
              </div>
            </SuccessCard>
          ) : (
            <form onSubmit={handleCryptoSubmit} className="space-y-4">
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
                <label className="text-xs text-gray-400 block mb-1">
                  Amount (INR equivalent)
                </label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 text-sm">
                    {"\u20B9"}
                  </span>
                  <input
                    type="number"
                    min="1"
                    step="1"
                    value={cryptoAmount}
                    onChange={(e) => setCryptoAmount(e.target.value)}
                    placeholder="Enter amount"
                    className="w-full h-11 pl-7 pr-3 bg-surface-light border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-lotus font-mono tabular-nums"
                  />
                </div>
                <div className="text-[10px] text-gray-500 mt-1">
                  Minimum: {formatMoney(MIN_DEPOSIT)}
                </div>
              </div>

              <button
                type="submit"
                disabled={cryptoSubmitting}
                className="w-full h-11 bg-lotus hover:bg-lotus-light text-white rounded-lg text-sm font-bold transition disabled:opacity-50"
              >
                {cryptoSubmitting ? "Processing..." : "Generate deposit address"}
              </button>
            </form>
          )}
        </div>
      )}

      {/* Footer note */}
      <div className="rounded-xl border border-gray-800 bg-surface/50 p-4 text-[11px] text-gray-400 leading-relaxed">
        <div className="font-semibold text-gray-300 mb-1">Deposit limits & processing</div>
        <ul className="list-disc pl-4 space-y-0.5">
          <li>Minimum deposit: {formatMoney(MIN_DEPOSIT)}, daily limit {formatMoney(500000)}</li>
          <li>UPI: typically credited within 5 minutes</li>
          <li>Bank transfer: 1-4 working hours after verification</li>
          <li>Crypto: credited after network confirmations (approx. 10-30 min)</li>
          <li>No deposit fees are charged by Lotus Exchange</li>
        </ul>
      </div>

      <Link
        href="/wallet"
        className="block text-center text-xs text-lotus hover:underline"
      >
        View deposit history
      </Link>
    </div>
  );
}

function SuccessCard({
  title,
  transactionId,
  status,
  onDone,
  children,
}: {
  title: string;
  transactionId: string;
  status: string;
  onDone: () => void;
  children?: React.ReactNode;
}) {
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-3">
        <div className="w-10 h-10 rounded-full bg-profit/15 text-profit flex items-center justify-center">
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
          </svg>
        </div>
        <div>
          <div className="text-sm font-bold text-white">{title}</div>
          <div className="text-[11px] text-gray-400">Status: {status}</div>
        </div>
      </div>
      <div className="rounded-lg border border-gray-800 bg-surface-light px-3 py-2 text-[11px]">
        <div className="text-gray-400">Transaction ID</div>
        <div className="text-white font-mono break-all">{transactionId}</div>
      </div>
      {children}
      <button
        type="button"
        onClick={onDone}
        className="w-full h-11 bg-surface-lighter hover:bg-surface-light border border-gray-700 text-white rounded-lg text-sm font-bold transition"
      >
        Done
      </button>
    </div>
  );
}
