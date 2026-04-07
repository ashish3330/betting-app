"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { useToast } from "@/components/Toast";

interface BankAccount {
  id: number;
  bank_name: string;
  account_holder: string;
  account_number: string;
  ifsc_code: string;
  upi_id?: string;
  qr_image_url?: string;
  daily_limit: number;
  status: string;
  used_today: number;
  remaining_limit: number;
  deposit_count_today: number;
  owner_role: string;
  owner_id: number;
  owner_username?: string;
}

interface DepositReq {
  id: number;
  player_id: number;
  player_username?: string;
  amount: number;
  status: string;
  bank_name?: string;
  account_last4?: string;
  created_at: string;
  txn_reference?: string;
}

interface UsageDashboard {
  date: string;
  total_accounts: number;
  total_used: number;
  total_remaining: number;
  total_deposits: number;
  accounts: BankAccount[];
}

export default function DepositsPanel() {
  const { addToast } = useToast();
  const [tab, setTab] = useState<"requests" | "accounts" | "usage" | "tree">("requests");
  const [requests, setRequests] = useState<DepositReq[]>([]);
  const [allRequests, setAllRequests] = useState<DepositReq[]>([]);
  const [accounts, setAccounts] = useState<BankAccount[]>([]);
  const [usage, setUsage] = useState<UsageDashboard | null>(null);
  const [tree, setTree] = useState<TreeNode | TreeNode[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [usageFilter, setUsageFilter] = useState("");
  const [reqFilter, setReqFilter] = useState<"pending" | "confirmed" | "rejected" | "all">("pending");

  useEffect(() => { loadData(); const i = setInterval(loadData, 10000); return () => clearInterval(i); }, []);

  async function loadData() {
    try {
      const [reqs, allReqs, accts, usg, treeData] = await Promise.all([
        api.request<DepositReq[]>("/api/v1/deposit/requests?status=pending", { auth: true }).catch(() => []),
        api.request<DepositReq[]>("/api/v1/deposit/requests", { auth: true }).catch(() => []),
        api.request<BankAccount[]>("/api/v1/deposit/accounts", { auth: true }).catch(() => []),
        api.request<UsageDashboard>(`/api/v1/deposit/usage${usageFilter ? `?owner_id=${usageFilter}` : ""}`, { auth: true }).catch(() => null),
        api.request<TreeNode | TreeNode[]>("/api/v1/deposit/tree", { auth: true }).catch(() => null),
      ]);
      setRequests(Array.isArray(reqs) ? reqs : []);
      setAllRequests(Array.isArray(allReqs) ? allReqs : []);
      setAccounts(Array.isArray(accts) ? accts : []);
      setTree(treeData);
      setUsage(usg);
    } catch {} finally { setLoading(false); }
  }

  async function confirmDeposit(id: number) {
    try {
      await api.request("/api/v1/deposit/confirm", { method: "POST", auth: true, body: JSON.stringify({ deposit_id: id }) });
      addToast({ type: "success", title: "Deposit confirmed & wallet credited" });
      loadData();
    } catch (err) {
      addToast({ type: "error", title: err instanceof Error ? err.message : "Failed" });
    }
  }

  async function rejectDeposit(id: number) {
    const reason = prompt("Rejection reason:");
    try {
      await api.request("/api/v1/deposit/reject", { method: "POST", auth: true, body: JSON.stringify({ deposit_id: id, reason: reason || "Rejected" }) });
      addToast({ type: "info", title: "Deposit rejected" });
      loadData();
    } catch (err) {
      addToast({ type: "error", title: err instanceof Error ? err.message : "Failed" });
    }
  }

  // Create bank account form state
  const [showCreate, setShowCreate] = useState(false);
  const [form, setForm] = useState({ bank_name: "", account_holder: "", account_number: "", ifsc_code: "", upi_id: "", qr_image_url: "" });

  async function createAccount() {
    if (!form.bank_name || !form.account_holder || !form.account_number || !form.ifsc_code) {
      addToast({ type: "warning", title: "Fill all required fields" }); return;
    }
    try {
      await api.request("/api/v1/deposit/accounts", { method: "POST", auth: true, body: JSON.stringify(form) });
      addToast({ type: "success", title: "Bank account created" });
      setShowCreate(false);
      setForm({ bank_name: "", account_holder: "", account_number: "", ifsc_code: "", upi_id: "", qr_image_url: "" });
      loadData();
    } catch (err) {
      addToast({ type: "error", title: err instanceof Error ? err.message : "Failed" });
    }
  }

  return (
    <div className="space-y-4 max-w-5xl">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-bold text-white">Deposit Management</h1>
          <p className="text-xs text-gray-500">{usage?.date || ""} IST</p>
        </div>
        <button onClick={() => setShowCreate(!showCreate)}
          className="bg-lotus hover:bg-lotus-light text-white text-xs px-4 py-2 rounded-lg font-medium transition">
          {showCreate ? "Cancel" : "+ Add Bank Account"}
        </button>
      </div>

      {/* Summary cards */}
      {usage && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
          <StatCard label="Accounts" value={usage.total_accounts.toString()} />
          <StatCard label="Used Today" value={`₹${usage.total_used.toLocaleString("en-IN")}`} color="text-orange-400" />
          <StatCard label="Remaining" value={`₹${usage.total_remaining.toLocaleString("en-IN")}`} color="text-profit" />
          <StatCard label="Deposits Today" value={usage.total_deposits.toString()} />
        </div>
      )}

      {/* Create bank account form */}
      {showCreate && (
        <div className="bg-surface rounded-lg border border-gray-800/60 p-4 space-y-3">
          <h3 className="text-sm font-semibold text-white">Add Bank Account</h3>
          <div className="grid grid-cols-2 gap-3">
            <Input label="Bank Name *" value={form.bank_name} onChange={(v) => setForm({...form, bank_name: v})} />
            <Input label="Account Holder *" value={form.account_holder} onChange={(v) => setForm({...form, account_holder: v})} />
            <Input label="Account Number *" value={form.account_number} onChange={(v) => setForm({...form, account_number: v})} />
            <Input label="IFSC Code *" value={form.ifsc_code} onChange={(v) => setForm({...form, ifsc_code: v})} placeholder="e.g. SBIN0001234" />
            <Input label="UPI ID" value={form.upi_id} onChange={(v) => setForm({...form, upi_id: v})} placeholder="name@upi" />
            <Input label="QR Image URL" value={form.qr_image_url} onChange={(v) => setForm({...form, qr_image_url: v})} placeholder="https://..." />
          </div>
          <button onClick={createAccount} className="bg-lotus hover:bg-lotus-light text-white text-xs px-6 py-2 rounded-lg font-medium transition">
            Create Account
          </button>
        </div>
      )}

      {/* Tabs */}
      <div className="flex gap-1 border-b border-gray-800/40">
        {(["requests", "accounts", "usage", "tree"] as const).map((t) => (
          <button key={t} onClick={() => setTab(t)}
            className={`px-4 py-2 text-xs font-medium capitalize transition ${tab === t ? "text-white border-b-2 border-lotus" : "text-gray-500"}`}>
            {t} {t === "requests" && requests.length > 0 ? `(${requests.length})` : ""}
          </button>
        ))}
      </div>

      {/* Tab content */}
      {tab === "requests" && (
        <div className="space-y-3">
          {/* Status filter pills */}
          <div className="flex gap-1">
            {(["pending", "confirmed", "rejected", "all"] as const).map((f) => {
              const count = f === "all" ? allRequests.length :
                f === "pending" ? requests.length :
                allRequests.filter(r => r.status === f).length;
              return (
                <button key={f} onClick={() => setReqFilter(f)}
                  className={`text-[10px] px-2.5 py-1 rounded-full font-medium transition ${
                    reqFilter === f ? "bg-lotus text-white" : "bg-surface-light text-gray-400 hover:text-white"
                  }`}>
                  {f.charAt(0).toUpperCase() + f.slice(1)} {count > 0 ? `(${count})` : ""}
                </button>
              );
            })}
          </div>

          {(() => {
            const filtered = reqFilter === "all" ? allRequests :
              reqFilter === "pending" ? requests :
              allRequests.filter(r => r.status === reqFilter);
            return filtered.length === 0 ? (
            <p className="text-center py-8 text-gray-500 text-sm">No {reqFilter} deposit requests</p>
          ) : filtered.map((req) => (
            <div key={req.id} className="bg-surface rounded-lg border border-gray-800/60 p-3 space-y-2">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <span className="text-xs text-white font-medium">#{req.id}</span>
                  <span className="text-xs text-gray-400">{req.player_username || `Player #${req.player_id}`}</span>
                  <span className="text-sm font-bold text-lotus tabular-nums">₹{req.amount.toLocaleString("en-IN")}</span>
                </div>
                <span className={`text-[9px] px-1.5 py-0.5 rounded font-medium ${
                  req.status === "pending" ? "bg-yellow-500/10 text-yellow-400" :
                  req.status === "confirmed" ? "bg-green-500/10 text-green-400" :
                  "bg-red-500/10 text-red-400"
                }`}>{req.status.toUpperCase()}</span>
              </div>
              {/* UTR prominently displayed */}
              {req.txn_reference && (
                <div className="bg-[var(--bg-primary)] rounded px-2.5 py-1.5 flex items-center justify-between">
                  <div>
                    <span className="text-[9px] text-gray-500">UTR / Txn Ref</span>
                    <p className="text-xs text-white font-mono font-bold tracking-wider">{req.txn_reference}</p>
                  </div>
                  <button onClick={() => { navigator.clipboard.writeText(req.txn_reference || ""); addToast({ type: "success", title: "UTR copied" }); }}
                    className="p-1 text-gray-400 hover:text-lotus transition">
                    <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                    </svg>
                  </button>
                </div>
              )}
              <div className="flex items-center justify-between">
                <div className="text-[10px] text-gray-500">
                  {req.bank_name} {req.account_last4 ? `••••${req.account_last4}` : ""} · {new Date(req.created_at).toLocaleString("en-IN", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" })}
                </div>
                {req.status === "pending" && (
                  <div className="flex gap-1">
                    <button onClick={() => confirmDeposit(req.id)}
                      className="text-[10px] px-3 py-1.5 bg-profit/20 text-profit hover:bg-profit/30 rounded font-medium transition">
                      Approve
                    </button>
                    <button onClick={() => rejectDeposit(req.id)}
                      className="text-[10px] px-3 py-1.5 bg-loss/20 text-loss hover:bg-loss/30 rounded font-medium transition">
                      Reject
                    </button>
                  </div>
                )}
              </div>
            </div>
          ));
          })()}
        </div>
      )}

      {tab === "accounts" && (
        <div className="space-y-1">
          {accounts.map((a) => (
            <div key={a.id} className="bg-surface rounded-lg border border-gray-800/60 p-3">
              <div className="flex items-center justify-between">
                <div>
                  <span className="text-sm font-medium text-white">{a.bank_name}</span>
                  <span className="text-[10px] text-gray-500 ml-2">****{a.account_number.slice(-4)}</span>
                  <span className={`text-[9px] ml-2 px-1.5 py-0.5 rounded ${a.status === "active" ? "bg-profit/20 text-profit" : "bg-gray-700 text-gray-400"}`}>
                    {a.status}
                  </span>
                </div>
                <span className="text-[10px] text-gray-500 capitalize">
                  {a.owner_username ? `${a.owner_username} (${a.owner_role})` : a.owner_role}
                </span>
              </div>
              <div className="flex items-center gap-4 mt-1 text-[10px] text-gray-400">
                <span>{a.account_holder}</span>
                <span>IFSC: {a.ifsc_code}</span>
                {a.upi_id && <span>UPI: {a.upi_id}</span>}
              </div>
            </div>
          ))}
          {accounts.length === 0 && <p className="text-center py-8 text-gray-500 text-sm">No bank accounts. Add one above.</p>}
        </div>
      )}

      {tab === "usage" && (
        <div className="space-y-2">
          {/* Filter by owner (for SuperAdmin) */}
          <div className="flex items-center gap-2">
            <input
              type="text"
              value={usageFilter}
              onChange={(e) => { setUsageFilter(e.target.value); }}
              onBlur={() => loadData()}
              onKeyDown={(e) => e.key === "Enter" && loadData()}
              placeholder="Filter by User ID (e.g. 2 for admin1)"
              className="h-8 px-3 text-xs bg-[var(--bg-primary)] border border-gray-700/60 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus w-64"
            />
            {usageFilter && (
              <button onClick={() => { setUsageFilter(""); setTimeout(loadData, 100); }}
                className="text-[10px] text-gray-400 hover:text-white">Clear</button>
            )}
            <span className="text-[10px] text-gray-500 ml-auto">{usage?.total_accounts || 0} accounts · ₹{(usage?.total_used || 0).toLocaleString("en-IN")} used</span>
          </div>
          {!usage?.accounts || usage.accounts.length === 0 ? (
            <p className="text-center py-8 text-gray-500 text-sm">No bank accounts found. Create one from the Accounts tab.</p>
          ) : usage.accounts.map((a) => {
            const pct = (a.used_today / a.daily_limit) * 100;
            const nearLimit = pct >= 80;
            const atLimit = pct >= 100;
            return (
              <div key={a.id} className={`bg-surface rounded-lg border p-3 ${atLimit ? "border-loss/40" : nearLimit ? "border-yellow-500/40" : "border-gray-800/60"}`}>
                <div className="flex items-center justify-between mb-1">
                  <span className="text-sm font-medium text-white">
                    {a.bank_name} ****{a.account_number.slice(-4)}
                    {a.owner_username && <span className="text-[9px] text-gray-500 ml-2">({a.owner_username})</span>}
                  </span>
                  <span className={`text-xs font-bold tabular-nums ${atLimit ? "text-loss" : nearLimit ? "text-yellow-400" : "text-profit"}`}>
                    ₹{a.used_today.toLocaleString("en-IN")} / ₹{a.daily_limit.toLocaleString("en-IN")}
                  </span>
                </div>
                {/* Progress bar */}
                <div className="w-full h-1.5 bg-gray-800 rounded-full overflow-hidden">
                  <div className={`h-full rounded-full transition-all ${atLimit ? "bg-loss" : nearLimit ? "bg-yellow-500" : "bg-profit"}`}
                    style={{ width: `${Math.min(pct, 100)}%` }} />
                </div>
                <div className="flex items-center justify-between mt-1 text-[9px] text-gray-500">
                  <span>{a.deposit_count_today} deposits today</span>
                  <span>Remaining: ₹{a.remaining_limit.toLocaleString("en-IN")}</span>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Tree tab — hierarchy with deposit stats */}
      {tab === "tree" && tree && (
        <div className="bg-surface rounded-lg border border-gray-800/60 p-3">
          {Array.isArray(tree) ? (
            tree.map((node) => <TreeNodeView key={node.id} node={node} depth={0} />)
          ) : (
            <TreeNodeView node={tree} depth={0} />
          )}
          {!tree && <p className="text-center py-8 text-gray-500 text-sm">No hierarchy data</p>}
        </div>
      )}
    </div>
  );
}

// ── Tree Node Types ──

interface TreeNode {
  id: number;
  username: string;
  role: string;
  balance: number;
  account_count: number;
  total_used_today: number;
  total_remaining: number;
  total_limit: number;
  deposits_pending: number;
  deposits_today: number;
  children?: TreeNode[];
}

// ── Tree Node Component ──

function TreeNodeView({ node, depth }: { node: TreeNode; depth: number }) {
  const [expanded, setExpanded] = useState(depth < 2);
  const hasChildren = node.children && node.children.length > 0;
  const pct = node.total_limit > 0 ? (node.total_used_today / node.total_limit) * 100 : 0;

  const isPlayer = node.role === "client";
  const roleBadge: Record<string, string> = {
    superadmin: "bg-red-500/20 text-red-400",
    admin: "bg-orange-500/20 text-orange-400",
    master: "bg-blue-500/20 text-blue-400",
    agent: "bg-green-500/20 text-green-400",
    client: "bg-purple-500/20 text-purple-400",
  };

  return (
    <div style={{ marginLeft: depth * 16 }}>
      <div className="flex items-center gap-2 py-1.5 border-b border-gray-800/20 last:border-0">
        {/* Expand/collapse */}
        {hasChildren ? (
          <button onClick={() => setExpanded(!expanded)} className="w-4 h-4 flex items-center justify-center text-gray-500 hover:text-white">
            <svg className={`w-3 h-3 transition-transform ${expanded ? "rotate-90" : ""}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
            </svg>
          </button>
        ) : (
          <span className="w-4 h-4 flex items-center justify-center text-gray-700">•</span>
        )}

        {/* User info */}
        <span className={`text-[9px] px-1.5 py-0.5 rounded font-medium capitalize ${roleBadge[node.role] || "bg-gray-700 text-gray-400"}`}>
          {node.role}
        </span>
        <span className="text-[12px] font-medium text-white">{node.username}</span>

        {/* Stats — different for players vs admin roles */}
        <div className="flex items-center gap-3 ml-auto text-[10px] tabular-nums">
          {isPlayer ? (
            <>
              <span className="text-profit font-medium">Bal: ₹{node.balance.toLocaleString("en-IN")}</span>
              {node.total_used_today > 0 && (
                <span className="text-gray-400">Deposited: ₹{node.total_used_today.toLocaleString("en-IN")}</span>
              )}
            </>
          ) : (
            <>
              {node.account_count > 0 && (
                <span className="text-gray-400">{node.account_count} acct{node.account_count > 1 ? "s" : ""}</span>
              )}
              {node.total_limit > 0 && (
                <span className={pct >= 90 ? "text-loss" : pct >= 70 ? "text-yellow-400" : "text-profit"}>
                  ₹{node.total_used_today.toLocaleString("en-IN")} / ₹{node.total_limit.toLocaleString("en-IN")}
                </span>
              )}
            </>
          )}
          {node.deposits_pending > 0 && (
            <span className="bg-yellow-500/20 text-yellow-400 px-1.5 py-0.5 rounded text-[9px] font-bold">
              {node.deposits_pending} pending
            </span>
          )}
          {node.deposits_today > 0 && (
            <span className="text-gray-500">{node.deposits_today} today</span>
          )}
        </div>
      </div>

      {/* Children */}
      {expanded && hasChildren && (
        <div className="border-l border-gray-800/30 ml-2">
          {node.children!.map((child) => (
            <TreeNodeView key={child.id} node={child} depth={depth + 1} />
          ))}
        </div>
      )}
    </div>
  );
}

function StatCard({ label, value, color = "text-white" }: { label: string; value: string; color?: string }) {
  return (
    <div className="bg-surface rounded-lg border border-gray-800/60 p-3">
      <div className="text-[10px] text-gray-500 uppercase tracking-wider">{label}</div>
      <div className={`text-lg font-bold font-mono ${color}`}>{value}</div>
    </div>
  );
}

function Input({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (v: string) => void; placeholder?: string }) {
  return (
    <div>
      <label className="text-[10px] text-gray-400">{label}</label>
      <input type="text" value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder}
        className="w-full mt-0.5 h-8 px-2 text-xs bg-[var(--bg-primary)] border border-gray-700/60 rounded text-white placeholder-gray-500 focus:outline-none focus:border-lotus" />
    </div>
  );
}
