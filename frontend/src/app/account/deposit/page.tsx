"use client";

import { useState, useEffect, useRef } from "react";
import { useAuth } from "@/lib/auth";
import { api } from "@/lib/api";
import Link from "next/link";
import { useToast } from "@/components/Toast";
import { createWorker } from "tesseract.js";

interface AvailableAccount {
  id: number;
  bank_name: string;
  account_holder: string;
  account_number: string;
  ifsc_code: string;
  upi_id: string;
  qr_image_url: string;
}

interface DepositRequest {
  id: number;
  amount: number;
  status: string;
  created_at: string;
  confirmed_at?: string;
  txn_reference?: string;
  bank_name?: string;
}

type Step = "amount" | "accounts" | "utr" | "done";

export default function DepositPage() {
  const { isLoggedIn, balance, refreshBalance } = useAuth();
  const { addToast } = useToast();

  const [step, setStep] = useState<Step>("amount");
  const [amount, setAmount] = useState("");
  const [accounts, setAccounts] = useState<AvailableAccount[]>([]);
  const [selectedAccount, setSelectedAccount] = useState<AvailableAccount | null>(null);
  const [utr, setUtr] = useState("");
  const [loading, setLoading] = useState(false);
  const [ocrLoading, setOcrLoading] = useState(false);
  const [accountsLoading, setAccountsLoading] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [deposits, setDeposits] = useState<DepositRequest[]>([]);
  const [historyLoading, setHistoryLoading] = useState(true);
  const [depositResult, setDepositResult] = useState<{ deposit_id: number } | null>(null);

  useEffect(() => {
    if (isLoggedIn) loadDeposits();
  }, [isLoggedIn]);

  async function loadDeposits() {
    try {
      const data = await api.request<DepositRequest[]>("/api/v1/deposit/requests", { auth: true });
      setDeposits(Array.isArray(data) ? data : []);
    } catch {
      // silent
    } finally {
      setHistoryLoading(false);
    }
  }

  async function loadAccounts() {
    setAccountsLoading(true);
    try {
      const data = await api.request<AvailableAccount[]>(`/api/v1/deposit/available-accounts?amount=${parseFloat(amount)}`, { auth: true });
      setAccounts(Array.isArray(data) ? data : []);
    } catch {
      addToast({ type: "error", title: "Failed to load accounts" });
    } finally {
      setAccountsLoading(false);
    }
  }

  function handleAmountSubmit() {
    const amt = parseFloat(amount);
    if (!amt || amt < 100) {
      addToast({ type: "error", title: "Minimum deposit is ₹100" });
      return;
    }
    if (amt > 90000) {
      addToast({ type: "error", title: "Maximum deposit is ₹90,000" });
      return;
    }
    loadAccounts();
    setStep("accounts");
  }

  function handleAccountSelect(acct: AvailableAccount) {
    setSelectedAccount(acct);
    setStep("utr");
  }

  function copyToClipboard(text: string, label: string) {
    navigator.clipboard.writeText(text);
    addToast({ type: "success", title: `${label} copied!` });
  }

  async function handleSubmitDeposit() {
    if (!utr || utr.length < 10) {
      addToast({ type: "error", title: "Please enter a valid UTR (minimum 10 characters)" });
      return;
    }
    setLoading(true);
    try {
      const data = await api.request<{ deposit_id: number }>("/api/v1/deposit/request", {
        method: "POST",
        auth: true,
        body: JSON.stringify({
          amount: parseFloat(amount),
          account_id: selectedAccount?.id,
          utr,
        }),
      });
      setDepositResult(data);
      setStep("done");
      addToast({ type: "success", title: "Deposit request submitted!", message: "Your agent will verify and approve." });
      loadDeposits();
    } catch (err) {
      addToast({ type: "error", title: "Deposit failed", message: err instanceof Error ? err.message : "Try again" });
    } finally {
      setLoading(false);
    }
  }

  function resetFlow() {
    setStep("amount");
    setAmount("");
    setSelectedAccount(null);
    setUtr("");
    setDepositResult(null);
    refreshBalance();
  }

  if (!isLoggedIn) {
    return (
      <div className="max-w-7xl mx-auto px-3 py-16 text-center">
        <h2 className="text-lg font-bold text-white">Please Login</h2>
        <Link href="/login" className="inline-block mt-4 bg-lotus text-white px-6 py-2 rounded-lg text-sm">Login</Link>
      </div>
    );
  }

  const quickAmounts = [500, 1000, 2000, 5000, 10000, 25000];

  return (
    <div className="max-w-lg mx-auto px-3 py-4 space-y-4">
      {/* Header */}
      <div className="flex items-center gap-2 text-xs text-gray-500">
        <Link href="/account" className="hover:text-white transition">Account</Link>
        <span>/</span>
        <span className="text-white">Deposit</span>
      </div>

      <h1 className="text-lg font-bold text-white">Deposit Funds</h1>

      {/* Balance */}
      {balance && (
        <div className="bg-surface rounded-lg border border-gray-800/60 p-3 flex items-center justify-between">
          <span className="text-xs text-gray-400">Available Balance</span>
          <span className="text-base font-bold text-profit font-mono">
            {"\u20B9"}{balance.available_balance?.toLocaleString("en-IN") ?? "0"}
          </span>
        </div>
      )}

      {/* Steps indicator */}
      <div className="flex items-center gap-1 text-[10px]">
        {["Amount", "Account", "UTR", "Done"].map((label, i) => {
          const stepIndex = ["amount", "accounts", "utr", "done"].indexOf(step);
          const isActive = i <= stepIndex;
          return (
            <div key={label} className="flex items-center gap-1 flex-1">
              <div className={`w-5 h-5 rounded-full flex items-center justify-center text-[9px] font-bold ${isActive ? "bg-lotus text-white" : "bg-gray-800 text-gray-500"}`}>
                {i + 1}
              </div>
              <span className={isActive ? "text-white" : "text-gray-500"}>{label}</span>
              {i < 3 && <div className={`flex-1 h-px ${isActive ? "bg-lotus/40" : "bg-gray-800"}`} />}
            </div>
          );
        })}
      </div>

      {/* ═══ STEP 1: Enter Amount ═══ */}
      {step === "amount" && (
        <div className="bg-surface rounded-lg border border-gray-800/60 p-4 space-y-3">
          <label className="text-xs text-gray-400">Deposit Amount ({"\u20B9"})</label>
          <input
            type="number"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            placeholder="Enter amount (min ₹100)"
            min="100" max="90000"
            className="w-full h-10 px-3 text-sm bg-[var(--bg-primary)] border border-gray-800 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus font-mono"
          />
          <div className="flex gap-1.5 flex-wrap">
            {quickAmounts.map((qa) => (
              <button key={qa} type="button" onClick={() => setAmount(qa.toString())}
                className="bg-surface-light hover:bg-surface-lighter text-[11px] text-gray-300 px-2.5 py-1 rounded transition">
                {"\u20B9"}{qa >= 1000 ? `${qa/1000}K` : qa}
              </button>
            ))}
          </div>
          <button onClick={handleAmountSubmit}
            className="w-full h-10 bg-lotus hover:bg-lotus-light text-white text-sm font-bold rounded-lg transition">
            Continue
          </button>
        </div>
      )}

      {/* ═══ STEP 2: Select Bank Account ═══ */}
      {step === "accounts" && (
        <div className="space-y-3">
          <div className="bg-lotus/10 border border-lotus/20 rounded-lg px-3 py-2">
            <p className="text-xs text-lotus font-medium">Deposit ₹{parseFloat(amount).toLocaleString("en-IN")}</p>
            <p className="text-[10px] text-gray-400 mt-0.5">Select a bank account to pay to. Top 2 accounts with highest available limit are shown.</p>
          </div>

          {accountsLoading ? (
            <div className="space-y-2">
              {[1,2].map(i => <div key={i} className="h-32 bg-surface rounded-lg border border-gray-800/60 animate-pulse" />)}
            </div>
          ) : accounts.length === 0 ? (
            <div className="bg-surface rounded-lg border border-gray-800/60 p-6 text-center">
              <p className="text-sm text-gray-400">No accounts available right now</p>
              <p className="text-[10px] text-gray-500 mt-1">All accounts have reached their daily limit. Try again tomorrow.</p>
            </div>
          ) : (
            accounts.map((acct) => (
              <button key={acct.id} onClick={() => handleAccountSelect(acct)}
                className="w-full text-left bg-surface rounded-lg border border-gray-800/60 p-3 hover:border-lotus/40 transition space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-sm font-medium text-white">{acct.bank_name}</span>
                  <span className="text-[10px] bg-lotus/10 text-lotus px-2 py-0.5 rounded">Available</span>
                </div>
                <div className="text-[11px] text-gray-400">{acct.account_holder}</div>
                <div className="grid grid-cols-2 gap-2 text-[10px]">
                  <div>
                    <span className="text-gray-500">A/C: </span>
                    <span className="text-gray-300 font-mono">••••{acct.account_number.slice(-4)}</span>
                  </div>
                  <div>
                    <span className="text-gray-500">IFSC: </span>
                    <span className="text-gray-300 font-mono">{acct.ifsc_code}</span>
                  </div>
                  <div className="col-span-2">
                    <span className="text-gray-500">UPI: </span>
                    <span className="text-lotus font-mono">{acct.upi_id}</span>
                  </div>
                </div>
              </button>
            ))
          )}
          <button onClick={() => setStep("amount")} className="text-xs text-gray-500 hover:text-white transition">
            ← Back to amount
          </button>
        </div>
      )}

      {/* ═══ STEP 3: Pay & Enter UTR ═══ */}
      {step === "utr" && selectedAccount && (
        <div className="space-y-3">
          {/* Payment details card */}
          <div className="bg-surface rounded-lg border border-lotus/30 p-4 space-y-3">
            <div className="text-center">
              <p className="text-xs text-gray-400">Pay exactly</p>
              <p className="text-2xl font-bold text-white font-mono">₹{parseFloat(amount).toLocaleString("en-IN")}</p>
              <p className="text-[10px] text-gray-500 mt-1">to {selectedAccount.bank_name}</p>
            </div>

            {/* QR Code */}
            {selectedAccount.qr_image_url && (
              <div className="flex justify-center">
                <img src={selectedAccount.qr_image_url} alt="QR Code" className="w-36 h-36 rounded-lg bg-white p-1" />
              </div>
            )}

            {/* Copyable details */}
            <div className="space-y-1.5">
              <CopyRow label="UPI ID" value={selectedAccount.upi_id} onCopy={copyToClipboard} />
              <CopyRow label="Account No" value={selectedAccount.account_number} onCopy={copyToClipboard} />
              <CopyRow label="IFSC" value={selectedAccount.ifsc_code} onCopy={copyToClipboard} />
              <CopyRow label="Name" value={selectedAccount.account_holder} onCopy={copyToClipboard} />
            </div>
          </div>

          {/* UTR via Screenshot Upload */}
          <div className="bg-surface rounded-lg border border-gray-800/60 p-4 space-y-3">
            <p className="text-xs text-gray-400 font-medium">After payment, upload payment screenshot</p>
            <p className="text-[10px] text-gray-500">We automatically extract the UTR from your screenshot. No images are saved on our servers.</p>

            {/* Hidden file input */}
            <input
              ref={fileInputRef}
              type="file"
              accept="image/*"
              capture="environment"
              className="hidden"
              onChange={async (e) => {
                const file = e.target.files?.[0];
                if (!file) return;
                setOcrLoading(true);
                addToast({ type: "info", title: "Reading screenshot...", message: "Extracting UTR from image" });
                try {
                  const worker = await createWorker("eng");
                  const { data: { text } } = await worker.recognize(file);
                  await worker.terminate();

                  // Send extracted text to backend for UTR + amount parsing
                  const result = await api.request<{ found: boolean; utr?: string; extracted_amount?: number }>("/api/v1/deposit/extract-utr", {
                    method: "POST",
                    body: JSON.stringify({ text }),
                  });

                  // Validate extracted amount against requested deposit amount
                  const requestedAmt = parseFloat(amount);
                  if (result.extracted_amount && result.extracted_amount > 0) {
                    if (Math.abs(result.extracted_amount - requestedAmt) > 1) {
                      addToast({
                        type: "error",
                        title: "Amount mismatch!",
                        message: `Screenshot shows \u20B9${result.extracted_amount.toLocaleString("en-IN")} but you requested \u20B9${requestedAmt.toLocaleString("en-IN")}. Please upload the correct screenshot.`,
                        duration: 8000,
                      });
                      setOcrLoading(false);
                      if (fileInputRef.current) fileInputRef.current.value = "";
                      return;
                    }
                  }

                  if (result.found && result.utr) {
                    setUtr(result.utr);
                    addToast({ type: "success", title: "UTR extracted!", message: result.utr });
                  } else {
                    addToast({ type: "warning", title: "Could not find UTR", message: "Please enter it manually below" });
                  }
                } catch {
                  addToast({ type: "error", title: "Failed to read screenshot" });
                } finally {
                  setOcrLoading(false);
                  if (fileInputRef.current) fileInputRef.current.value = "";
                }
              }}
            />

            {/* Upload button */}
            <button
              type="button"
              onClick={() => fileInputRef.current?.click()}
              disabled={ocrLoading}
              className="w-full h-12 bg-surface-light hover:bg-surface-lighter border-2 border-dashed border-gray-700 hover:border-lotus/40 text-gray-300 rounded-lg transition flex items-center justify-center gap-2 disabled:opacity-50"
            >
              {ocrLoading ? (
                <>
                  <span className="w-4 h-4 border-2 border-gray-500 border-t-lotus rounded-full animate-spin" />
                  <span className="text-xs">Extracting UTR...</span>
                </>
              ) : (
                <>
                  <svg className="w-5 h-5 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
                  </svg>
                  <span className="text-xs font-medium">Upload Payment Screenshot</span>
                </>
              )}
            </button>

            {/* UTR field — auto-filled from OCR or manual fallback */}
            <div>
              <label className="text-[10px] text-gray-500 mb-1 block">UTR / Transaction Reference</label>
              <input
                type="text"
                value={utr}
                onChange={(e) => setUtr(e.target.value.replace(/[^a-zA-Z0-9]/g, ""))}
                placeholder="Auto-filled from screenshot or enter manually"
                className="w-full h-10 px-3 text-sm bg-[var(--bg-primary)] border border-gray-800 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus font-mono tracking-wider"
              />
            </div>

            {utr && (
              <div className="bg-lotus/10 border border-lotus/20 rounded-lg px-3 py-2 flex items-center gap-2">
                <svg className="w-4 h-4 text-lotus flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <span className="text-xs text-lotus font-mono">{utr}</span>
              </div>
            )}

            <button onClick={handleSubmitDeposit} disabled={loading || !utr}
              className="w-full h-10 bg-lotus hover:bg-lotus-light disabled:bg-gray-700 disabled:text-gray-500 text-white text-sm font-bold rounded-lg transition">
              {loading ? "Submitting..." : "Submit Deposit Request"}
            </button>
          </div>

          <button onClick={() => { setStep("accounts"); setSelectedAccount(null); setUtr(""); }}
            className="text-xs text-gray-500 hover:text-white transition">
            ← Choose different account
          </button>
        </div>
      )}

      {/* ═══ STEP 4: Done ═══ */}
      {step === "done" && (
        <div className="bg-surface rounded-lg border border-green-500/30 p-6 text-center space-y-3">
          <div className="w-12 h-12 bg-green-500/20 rounded-full flex items-center justify-center mx-auto">
            <svg className="w-6 h-6 text-green-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
            </svg>
          </div>
          <h3 className="text-sm font-bold text-white">Deposit Request Submitted</h3>
          <p className="text-xs text-gray-400">
            Your deposit of <span className="text-white font-mono">₹{parseFloat(amount).toLocaleString("en-IN")}</span> is pending approval.
          </p>
          <p className="text-[10px] text-gray-500">UTR: <span className="font-mono text-gray-300">{utr}</span></p>
          {depositResult && (
            <p className="text-[10px] text-gray-500">Request ID: <span className="font-mono text-gray-300">#{depositResult.deposit_id}</span></p>
          )}
          <p className="text-[10px] text-lotus">Your agent will verify the payment and credit your wallet.</p>
          <button onClick={resetFlow}
            className="mt-2 bg-lotus hover:bg-lotus-light text-white text-sm px-6 py-2 rounded-lg transition">
            Make Another Deposit
          </button>
        </div>
      )}

      {/* ═══ Recent Deposits ═══ */}
      <div className="bg-surface rounded-lg border border-gray-800/60 p-4">
        <h2 className="text-sm font-bold text-white mb-3">Recent Deposits</h2>
        {historyLoading ? (
          <div className="space-y-2">
            {[1,2,3].map(i => <div key={i} className="h-12 bg-surface-light rounded-lg animate-pulse" />)}
          </div>
        ) : deposits.length === 0 ? (
          <p className="text-xs text-gray-500 text-center py-4">No deposits yet</p>
        ) : (
          <div className="space-y-1.5">
            {deposits.slice(0, 10).map((dep) => (
              <div key={dep.id} className="flex items-center justify-between py-2 border-b border-gray-800/30 last:border-0">
                <div>
                  <p className="text-sm text-white font-mono">₹{dep.amount?.toLocaleString("en-IN")}</p>
                  <p className="text-[10px] text-gray-500">
                    {dep.txn_reference && <span className="font-mono">UTR: {dep.txn_reference} · </span>}
                    {new Date(dep.created_at).toLocaleString("en-IN", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" })}
                  </p>
                </div>
                <span className={`text-[10px] px-2 py-0.5 rounded font-medium ${
                  dep.status === "confirmed" ? "bg-green-500/10 text-green-400" :
                  dep.status === "pending" ? "bg-yellow-500/10 text-yellow-400" :
                  "bg-red-500/10 text-red-400"
                }`}>
                  {dep.status?.toUpperCase()}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function CopyRow({ label, value, onCopy }: { label: string; value: string; onCopy: (v: string, l: string) => void }) {
  return (
    <div className="flex items-center justify-between bg-[var(--bg-primary)] rounded-lg px-3 py-2">
      <div className="min-w-0">
        <p className="text-[9px] text-gray-500">{label}</p>
        <p className="text-[11px] text-white font-mono truncate">{value}</p>
      </div>
      <button onClick={() => onCopy(value, label)}
        className="flex-shrink-0 ml-2 p-1.5 text-gray-400 hover:text-lotus transition rounded hover:bg-white/5">
        <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
        </svg>
      </button>
    </div>
  );
}
