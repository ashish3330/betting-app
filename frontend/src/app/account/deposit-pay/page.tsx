"use client";

import { useState } from "react";
import { useAuth } from "@/lib/auth";
import { api } from "@/lib/api";
import { useToast } from "@/components/Toast";
import Link from "next/link";

interface PayToDetails {
  bank_name: string;
  account_holder: string;
  account_number: string;
  ifsc_code: string;
  upi_id?: string;
  qr_image_url?: string;
}

interface DepositResponse {
  deposit_id: number;
  amount: number;
  status: string;
  pay_to: PayToDetails;
  message: string;
}

export default function DepositPayPage() {
  const { isLoggedIn } = useAuth();
  const { addToast } = useToast();
  const [amount, setAmount] = useState("");
  const [loading, setLoading] = useState(false);
  const [depositInfo, setDepositInfo] = useState<DepositResponse | null>(null);

  if (!isLoggedIn) {
    return (
      <div className="max-w-lg mx-auto px-3 py-16 text-center">
        <p className="text-gray-400">Please login to make a deposit</p>
        <Link href="/login" className="text-lotus mt-2 inline-block">Login</Link>
      </div>
    );
  }

  async function handleRequest() {
    const amt = parseFloat(amount);
    if (!amt || amt < 100) {
      addToast({ type: "warning", title: "Minimum deposit is ₹100" });
      return;
    }
    setLoading(true);
    try {
      const data = await api.request<DepositResponse>("/api/v1/deposit/request", {
        method: "POST",
        auth: true,
        body: JSON.stringify({ amount: amt }),
      });
      setDepositInfo(data);
      addToast({ type: "success", title: "Deposit request created" });
    } catch (err) {
      addToast({ type: "error", title: err instanceof Error ? err.message : "Failed" });
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="max-w-lg mx-auto px-3 py-4 space-y-4">
      <div className="flex items-center gap-2 text-xs text-gray-500">
        <Link href="/account" className="hover:text-white transition">Account</Link>
        <span>/</span>
        <span className="text-white">Deposit</span>
      </div>

      <h1 className="text-lg font-bold text-white">Deposit Funds</h1>

      {!depositInfo ? (
        /* Step 1: Enter amount */
        <div className="bg-surface rounded-lg border border-gray-800/60 p-4 space-y-3">
          <div>
            <label className="text-xs text-gray-400">Deposit Amount (₹)</label>
            <input
              type="number"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              min="100"
              max="90000"
              placeholder="Min ₹100"
              className="w-full mt-1 h-10 px-3 text-sm bg-[var(--bg-primary)] border border-gray-700/60 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus"
            />
          </div>
          <div className="flex gap-2">
            {[500, 1000, 5000, 10000, 25000].map((a) => (
              <button key={a} onClick={() => setAmount(String(a))}
                className="flex-1 text-[10px] py-1.5 bg-surface-light hover:bg-surface-lighter border border-gray-800/60 rounded text-gray-400 hover:text-white transition">
                ₹{a >= 1000 ? `${a/1000}K` : a}
              </button>
            ))}
          </div>
          <button
            onClick={handleRequest}
            disabled={loading}
            className="w-full h-10 bg-lotus hover:bg-lotus-light text-white text-sm font-semibold rounded-lg transition disabled:opacity-50"
          >
            {loading ? "Finding account..." : "Request Deposit"}
          </button>
          <p className="text-[10px] text-gray-500 text-center">
            System will assign a bank account for you to transfer to
          </p>
        </div>
      ) : (
        /* Step 2: Show payment details */
        <div className="space-y-3">
          <div className="bg-lotus/10 border border-lotus/30 rounded-lg p-3 text-center">
            <p className="text-xs text-lotus font-medium">Transfer exactly</p>
            <p className="text-2xl font-bold text-white mt-1">₹{depositInfo.amount.toLocaleString("en-IN")}</p>
            <p className="text-[10px] text-gray-400 mt-1">Deposit ID: #{depositInfo.deposit_id}</p>
          </div>

          <div className="bg-surface rounded-lg border border-gray-800/60 p-4 space-y-3">
            <h3 className="text-sm font-semibold text-white">Bank Details</h3>
            <InfoRow label="Bank" value={depositInfo.pay_to.bank_name} />
            <InfoRow label="Account Holder" value={depositInfo.pay_to.account_holder} />
            <InfoRow label="Account Number" value={depositInfo.pay_to.account_number} copy />
            <InfoRow label="IFSC Code" value={depositInfo.pay_to.ifsc_code} copy />
            {depositInfo.pay_to.upi_id && (
              <InfoRow label="UPI ID" value={depositInfo.pay_to.upi_id} copy />
            )}
          </div>

          {depositInfo.pay_to.qr_image_url && (
            <div className="bg-surface rounded-lg border border-gray-800/60 p-4 text-center">
              <p className="text-xs text-gray-400 mb-2">Scan QR to pay</p>
              <div className="w-48 h-48 mx-auto bg-white rounded-lg flex items-center justify-center">
                <img src={depositInfo.pay_to.qr_image_url} alt="QR Code" className="w-44 h-44 object-contain" />
              </div>
            </div>
          )}

          <div className="bg-yellow-500/10 border border-yellow-500/30 rounded-lg p-3">
            <p className="text-xs text-yellow-400 font-medium">After payment</p>
            <p className="text-[11px] text-gray-400 mt-1">
              Your Agent will verify the transaction and credit your wallet. This usually takes 5-15 minutes.
            </p>
          </div>

          <button
            onClick={() => setDepositInfo(null)}
            className="w-full py-2 text-xs text-gray-400 hover:text-white transition"
          >
            Make another deposit
          </button>
        </div>
      )}
    </div>
  );
}

function InfoRow({ label, value, copy }: { label: string; value: string; copy?: boolean }) {
  const { addToast } = useToast();
  return (
    <div className="flex items-center justify-between py-1.5 border-b border-gray-800/30 last:border-0">
      <span className="text-[11px] text-gray-500">{label}</span>
      <div className="flex items-center gap-2">
        <span className="text-[12px] text-white font-mono">{value}</span>
        {copy && (
          <button
            onClick={() => { navigator.clipboard.writeText(value); addToast({ type: "info", title: "Copied" }); }}
            className="text-[9px] text-lotus hover:text-lotus-light transition"
          >
            Copy
          </button>
        )}
      </div>
    </div>
  );
}
