"use client";

import { useEffect, useState } from "react";
import { useAuth } from "@/lib/auth";
import { api, User } from "@/lib/api";
import Select from "@/components/Select";

export default function CreditManagementPage() {
  const { user: me, balance } = useAuth();
  const [children, setChildren] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedChild, setSelectedChild] = useState<number | null>(null);
  const [amount, setAmount] = useState(0);
  const [transferring, setTransferring] = useState(false);
  const [message, setMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);

  useEffect(() => {
    loadChildren();
  }, []);

  async function loadChildren() {
    try {
      const data = await api.request<User[]>("/api/v1/hierarchy/children/direct", { auth: true });
      setChildren(Array.isArray(data) ? data : []);
    } catch {
      setChildren([]);
    } finally {
      setLoading(false);
    }
  }

  async function handleTransfer() {
    if (!selectedChild || amount <= 0) return;
    setTransferring(true);
    setMessage(null);

    try {
      await api.request("/api/v1/panel/credit/transfer", {
        method: "POST",
        auth: true,
        body: JSON.stringify({ to_user_id: selectedChild, amount }),
      });
      setMessage({ type: "success", text: `₹${amount.toLocaleString("en-IN")} transferred successfully` });
      setAmount(0);
      loadChildren(); // Refresh balances
    } catch (err) {
      setMessage({ type: "error", text: err instanceof Error ? err.message : "Transfer failed" });
    } finally {
      setTransferring(false);
    }
  }

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h1 className="text-xl font-bold text-white">Credit Management</h1>
        <p className="text-sm text-gray-500">Transfer credit to your direct children</p>
      </div>

      {/* Own Balance */}
      <div className="bg-surface rounded-lg border border-gray-800/60 p-4">
        <div className="text-[10px] text-gray-500 uppercase tracking-wider">Your Available Balance</div>
        <div className="text-2xl font-bold font-mono text-profit mt-1">
          ₹{(balance?.available_balance || me?.balance || 0).toLocaleString("en-IN")}
        </div>
        <div className="flex gap-4 mt-2 text-xs text-gray-500">
          <span>Balance: ₹{(balance?.balance || 0).toLocaleString("en-IN")}</span>
          <span>Exposure: ₹{(balance?.exposure || 0).toLocaleString("en-IN")}</span>
        </div>
      </div>

      {/* Transfer Form */}
      <div className="bg-surface rounded-lg border border-gray-800/60 p-4 space-y-4">
        <h2 className="text-sm font-semibold text-white">Transfer Credit</h2>

        {message && (
          <div className={`text-xs px-3 py-2 rounded-lg border ${
            message.type === "success" ? "bg-profit/10 border-profit/30 text-profit" : "bg-loss/10 border-loss/30 text-loss"
          }`}>
            {message.text}
          </div>
        )}

        <div>
          <label className="text-xs text-gray-400 mb-1 block">Select User</label>
          <Select
            value={String(selectedChild || "")}
            onChange={(v) => setSelectedChild(parseInt(v) || null)}
            placeholder="Choose a direct child"
            options={children.map((c) => ({
              value: String(c.id),
              label: `${c.username} (${c.role})`,
              subtitle: `Balance: ₹${(c.balance || 0).toLocaleString("en-IN")}`,
            }))}
          />
        </div>

        <div>
          <label className="text-xs text-gray-400 mb-1 block">Amount (₹)</label>
          <input
            type="number"
            value={amount || ""}
            onChange={(e) => setAmount(parseFloat(e.target.value) || 0)}
            min={1}
            placeholder="Enter amount"
            className="w-full h-9 px-3 text-sm bg-[var(--bg-primary)] border border-gray-800 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus font-mono"
          />
          <div className="flex gap-2 mt-1.5">
            {[1000, 5000, 10000, 50000, 100000].map((a) => (
              <button
                key={a}
                onClick={() => setAmount(a)}
                className="text-[10px] px-2 py-1 bg-surface-light hover:bg-surface-lighter border border-gray-800 rounded text-gray-400 hover:text-white transition"
              >
                {a >= 100000 ? `${a / 100000}L` : `${a / 1000}K`}
              </button>
            ))}
          </div>
        </div>

        <button
          onClick={handleTransfer}
          disabled={transferring || !selectedChild || amount <= 0}
          className="w-full h-10 bg-lotus hover:bg-lotus-light disabled:bg-gray-700 disabled:text-gray-500 text-white text-sm font-bold rounded-lg transition"
        >
          {transferring ? "Transferring..." : `Transfer ₹${amount.toLocaleString("en-IN")}`}
        </button>
      </div>

      {/* Children Balances */}
      <div>
        <h2 className="text-sm font-semibold text-gray-400 mb-2 uppercase tracking-wider">Direct Children</h2>
        {loading ? (
          <div className="space-y-2">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="h-12 bg-surface rounded-lg animate-pulse" />
            ))}
          </div>
        ) : children.length === 0 ? (
          <div className="text-center py-8 text-gray-500 text-sm">
            No children yet. <a href="/panel/create-user" className="text-lotus hover:underline">Create one</a>
          </div>
        ) : (
          <div className="space-y-1">
            {children.map((c) => (
              <div key={c.id} className="flex items-center justify-between bg-surface rounded-lg border border-gray-800/60 p-3">
                <div>
                  <div className="text-sm font-medium text-white">{c.username}</div>
                  <div className="text-[10px] text-gray-500 capitalize">{c.role}</div>
                </div>
                <div className="text-right">
                  <div className="text-sm font-mono text-profit">₹{(c.balance || 0).toLocaleString("en-IN")}</div>
                  <div className="text-[10px] text-gray-500">Exp: ₹{(c.exposure || 0).toLocaleString("en-IN")}</div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
