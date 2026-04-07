"use client";

import { useEffect, useState } from "react";
import { api, WalletBalance, LedgerEntry } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useRouter } from "next/navigation";
import Pagination from "@/components/Pagination";

const LEDGER_PER_PAGE = 20;

export default function WalletPage() {
  const { isLoggedIn, isLoading: authLoading } = useAuth();
  const router = useRouter();
  const [balance, setBalance] = useState<WalletBalance | null>(null);
  const [ledger, setLedger] = useState<LedgerEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [ledgerPage, setLedgerPage] = useState(1);

  useEffect(() => {
    if (!authLoading && !isLoggedIn) {
      router.push("/login");
      return;
    }
    if (isLoggedIn) {
      loadData();
    }
  }, [isLoggedIn, authLoading, router]);

  async function loadData() {
    try {
      const [bal, led] = await Promise.all([
        api.getBalance(),
        api.getLedger().catch(() => []),
      ]);
      setBalance(bal);
      setLedger(Array.isArray(led) ? led : []);
    } catch {
      // API not available
    } finally {
      setLoading(false);
    }
  }

  if (authLoading || loading) {
    return (
      <div className="max-w-2xl mx-auto px-3 py-4 space-y-4">
        <div className="bg-surface rounded-xl border border-gray-800 h-40 animate-pulse" />
        <div className="bg-surface rounded-xl border border-gray-800 h-60 animate-pulse" />
      </div>
    );
  }

  return (
    <div className="max-w-2xl mx-auto px-3 py-4 space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-bold text-white">Wallet</h1>
        <div className="flex gap-2">
          <button
            onClick={() => router.push("/wallet/deposit")}
            className="px-4 py-1.5 bg-profit/20 text-profit hover:bg-profit/30 border border-profit/30 rounded-lg text-xs font-bold transition"
          >
            Deposit
          </button>
          <button
            onClick={() => router.push("/wallet/withdraw")}
            className="px-4 py-1.5 bg-surface-lighter text-gray-300 hover:text-white border border-gray-700 rounded-lg text-xs font-bold transition"
          >
            Withdraw
          </button>
        </div>
      </div>

      {/* Balance Cards */}
      <div className="grid grid-cols-3 gap-2">
        <BalanceCard
          label="Balance"
          value={balance?.balance ?? 0}
          accent="text-white"
        />
        <BalanceCard
          label="Exposure"
          value={balance?.exposure ?? 0}
          accent="text-loss"
        />
        <BalanceCard
          label="Available"
          value={balance?.available_balance ?? 0}
          accent="text-profit"
        />
      </div>

      {/* Summary */}
      {balance && (
        <div className="bg-surface rounded-xl border border-gray-800 p-4">
          <div className="space-y-2">
            <div className="flex justify-between text-sm">
              <span className="text-gray-400">Total Balance</span>
              <span className="text-white font-mono font-bold">
                {"\u20B9"}
                {balance.balance.toLocaleString("en-IN", {
                  minimumFractionDigits: 2,
                })}
              </span>
            </div>
            <div className="flex justify-between text-sm">
              <span className="text-gray-400">Current Exposure</span>
              <span className="text-loss font-mono font-bold">
                - {"\u20B9"}
                {Math.abs(balance.exposure).toLocaleString("en-IN", {
                  minimumFractionDigits: 2,
                })}
              </span>
            </div>
            <div className="border-t border-gray-800 pt-2 flex justify-between text-sm">
              <span className="text-gray-300 font-medium">
                Available to Bet
              </span>
              <span className="text-profit font-mono font-bold">
                {"\u20B9"}
                {balance.available_balance.toLocaleString("en-IN", {
                  minimumFractionDigits: 2,
                })}
              </span>
            </div>
          </div>
        </div>
      )}

      {/* Ledger */}
      <div className="bg-surface rounded-xl border border-gray-800 overflow-hidden">
        <div className="px-3 py-2 border-b border-gray-800">
          <h2 className="text-xs font-medium text-gray-400 uppercase tracking-wide">
            Transaction History
          </h2>
        </div>

        {ledger.length > 0 ? (
          <>
            <div className="divide-y divide-gray-800/50">
              {ledger
                .slice((ledgerPage - 1) * LEDGER_PER_PAGE, ledgerPage * LEDGER_PER_PAGE)
                .map((entry) => (
                <div key={entry.id} className="px-3 py-2.5 flex items-center justify-between">
                  <div>
                    <div className="text-sm text-white">{entry.description}</div>
                    <div className="text-[10px] text-gray-500 mt-0.5">
                      {entry.type} |{" "}
                      {new Date(entry.created_at).toLocaleString("en-IN", {
                        day: "numeric",
                        month: "short",
                        hour: "2-digit",
                        minute: "2-digit",
                      })}
                    </div>
                  </div>
                  <div className="text-right">
                    <div
                      className={`text-sm font-mono font-bold ${
                        entry.amount >= 0 ? "text-profit" : "text-loss"
                      }`}
                    >
                      {entry.amount >= 0 ? "+" : ""}
                      {"\u20B9"}
                      {Math.abs(entry.amount).toLocaleString("en-IN", {
                        minimumFractionDigits: 2,
                      })}
                    </div>
                    <div className="text-[10px] text-gray-500 font-mono">
                      Bal: {"\u20B9"}
                      {entry.balance_after?.toLocaleString("en-IN", {
                        minimumFractionDigits: 2,
                      })}
                    </div>
                  </div>
                </div>
              ))}
            </div>
            <div className="px-3 pb-3">
              <Pagination
                currentPage={ledgerPage}
                totalPages={Math.ceil(ledger.length / LEDGER_PER_PAGE)}
                onPageChange={setLedgerPage}
              />
            </div>
          </>
        ) : (
          <div className="text-center py-12 text-gray-500 text-sm">
            No transactions yet
          </div>
        )}
      </div>
    </div>
  );
}

function BalanceCard({
  label,
  value,
  accent,
}: {
  label: string;
  value: number;
  accent: string;
}) {
  return (
    <div className="bg-surface rounded-xl border border-gray-800 p-3 text-center">
      <div className="text-[10px] text-gray-500 uppercase mb-1">{label}</div>
      <div className={`text-sm sm:text-base font-bold font-mono ${accent}`}>
        {"\u20B9"}
        {value.toLocaleString("en-IN", {
          minimumFractionDigits: 0,
          maximumFractionDigits: 0,
        })}
      </div>
    </div>
  );
}
