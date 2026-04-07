"use client";
import { useToast } from "@/components/Toast";

import { useEffect, useState } from "react";
import { useAuth } from "@/lib/auth";
import { api, User } from "@/lib/api";
import Pagination from "@/components/Pagination";
import Select from "@/components/Select";

const USERS_PER_PAGE = 20;

export default function PanelUsersPage() {
  const { user: me } = useAuth();
  const { addToast } = useToast();
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [filterRole, setFilterRole] = useState("");
  const [search, setSearch] = useState("");
  const [currentPage, setCurrentPage] = useState(1);

  useEffect(() => {
    loadUsers();
  }, [filterRole]);

  async function loadUsers() {
    setLoading(true);
    try {
      const url = filterRole ? `/api/v1/panel/users?role=${filterRole}` : "/api/v1/panel/users";
      const data = await api.request<User[]>(url, { auth: true });
      setUsers(Array.isArray(data) ? data : []);
    } catch {
      setUsers([]);
    } finally {
      setLoading(false);
    }
  }

  const [refillUserId, setRefillUserId] = useState<number | null>(null);
  const [refillAmount, setRefillAmount] = useState("");
  const [refilling, setRefilling] = useState(false);

  async function toggleStatus(userId: number, currentStatus: string) {
    const newStatus = currentStatus === "active" ? "suspended" : "active";
    try {
      await api.request(`/api/v1/panel/user/${userId}/status`, {
        method: "POST",
        auth: true,
        body: JSON.stringify({ status: newStatus }),
      });
      setUsers((prev) =>
        prev.map((u) => (u.id === userId ? { ...u, status: newStatus } : u))
      );
    } catch {
      addToast({ type: "error", title: "Failed to update status" });
    }
  }

  async function refillUser(userId: number) {
    const amount = parseFloat(refillAmount);
    if (!amount || amount <= 0) { addToast({ type: "warning", title: "Enter a valid amount" }); return; }
    setRefilling(true);
    try {
      await api.request("/api/v1/panel/credit/transfer", {
        method: "POST",
        auth: true,
        body: JSON.stringify({ to_user_id: userId, amount }),
      });
      // Update balance in UI
      setUsers((prev) =>
        prev.map((u) => (u.id === userId ? { ...u, balance: (u.balance || 0) + amount } : u))
      );
      setRefillUserId(null);
      setRefillAmount("");
    } catch (err) {
      addToast({ type: "error", title: err instanceof Error ? err.message : "Transfer failed" });
    } finally {
      setRefilling(false);
    }
  }

  const role = me?.role || "";
  const roleOptions: Record<string, string[]> = {
    superadmin: ["admin", "master", "agent", "client"],
    admin: ["master", "agent", "client"],
    master: ["agent", "client"],
    agent: ["client"],
  };

  const filtered = users.filter((u) => {
    if (search && !u.username.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

  const totalPages = Math.ceil(filtered.length / USERS_PER_PAGE);
  const paginatedUsers = filtered.slice(
    (currentPage - 1) * USERS_PER_PAGE,
    currentPage * USERS_PER_PAGE
  );

  // Reset page when filters change
  useEffect(() => {
    setCurrentPage(1);
  }, [search, filterRole]);

  const roleBadge: Record<string, string> = {
    superadmin: "bg-red-500/20 text-red-400",
    admin: "bg-orange-500/20 text-orange-400",
    master: "bg-blue-500/20 text-blue-400",
    agent: "bg-green-500/20 text-green-400",
    client: "bg-gray-500/20 text-gray-400",
  };

  return (
    <div className="space-y-4 max-w-5xl">
      <div className="flex items-center justify-between flex-wrap gap-2">
        <div>
          <h1 className="text-xl font-bold text-white">My Downline</h1>
          <p className="text-sm text-gray-500">{filtered.length} users</p>
        </div>
        <a href="/panel/create-user" className="bg-lotus hover:bg-lotus-light text-white text-xs px-4 py-2 rounded-lg font-medium transition">
          + Create User
        </a>
      </div>

      {/* Filters */}
      <div className="flex gap-2 flex-wrap">
        <input
          type="text"
          placeholder="Search username..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="h-8 px-3 text-xs bg-surface border border-gray-800 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus w-48"
        />
        <Select
          value={filterRole}
          onChange={setFilterRole}
          placeholder="All Roles"
          options={[
            { value: "", label: "All Roles" },
            ...(roleOptions[role] || []).map((r) => ({ value: r, label: r })),
          ]}
          className="w-36"
        />
      </div>

      {/* Users Table */}
      {loading ? (
        <div className="space-y-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="h-12 bg-surface rounded-lg animate-pulse" />
          ))}
        </div>
      ) : filtered.length === 0 ? (
        <div className="text-center py-12 text-gray-500">
          <p className="text-lg">No users found</p>
          <p className="text-sm mt-1">Create users from the Create User page</p>
        </div>
      ) : (
        <div className="bg-surface rounded-lg border border-gray-800/60 overflow-hidden">
          {/* Header */}
          <div className="grid grid-cols-12 gap-2 px-3 py-2 border-b border-gray-800/40 text-[10px] text-gray-500 font-semibold uppercase tracking-wider">
            <div className="col-span-1">ID</div>
            <div className="col-span-2">Username</div>
            <div className="col-span-1">Role</div>
            <div className="col-span-2">Balance</div>
            <div className="col-span-2">Exposure</div>
            <div className="col-span-2">Status</div>
            <div className="col-span-2">Actions</div>
          </div>

          {/* Rows */}
          {paginatedUsers.map((u) => (
            <div key={u.id} className="grid grid-cols-12 gap-2 px-3 py-2 border-b border-gray-800/20 items-center hover:bg-surface-light/30 transition text-xs">
              <div className="col-span-1 text-gray-500 font-mono">{u.id}</div>
              <div className="col-span-2 text-white font-medium truncate">{u.username}</div>
              <div className="col-span-1">
                <span className={`text-[10px] px-1.5 py-0.5 rounded font-medium capitalize ${roleBadge[u.role] || "bg-gray-700 text-gray-400"}`}>
                  {u.role}
                </span>
              </div>
              <div className="col-span-2 text-profit font-mono">₹{(u.balance || 0).toLocaleString("en-IN")}</div>
              <div className="col-span-2 text-orange-400 font-mono">₹{(u.exposure || 0).toLocaleString("en-IN")}</div>
              <div className="col-span-2">
                <span className={`text-[10px] px-1.5 py-0.5 rounded font-medium ${
                  u.status === "active" ? "bg-profit/20 text-profit" :
                  u.status === "suspended" ? "bg-yellow-500/20 text-yellow-400" :
                  "bg-loss/20 text-loss"
                }`}>
                  {u.status}
                </span>
              </div>
              <div className="col-span-2 flex gap-1 flex-wrap">
                <button
                  onClick={() => toggleStatus(u.id as number, u.status)}
                  className={`text-[10px] px-2 py-1 rounded font-medium transition ${
                    u.status === "active"
                      ? "bg-yellow-500/10 text-yellow-400 hover:bg-yellow-500/20"
                      : "bg-profit/10 text-profit hover:bg-profit/20"
                  }`}
                >
                  {u.status === "active" ? "Suspend" : "Activate"}
                </button>
                {/* Refill button */}
                {u.parent_id && (
                  <button
                    onClick={() => setRefillUserId(refillUserId === (u.id as number) ? null : (u.id as number))}
                    className="text-[10px] px-2 py-1 rounded font-medium bg-lotus/10 text-lotus hover:bg-lotus/20 transition"
                  >
                    {refillUserId === u.id ? "Cancel" : "Refill"}
                  </button>
                )}
              </div>
              {/* Inline refill form */}
              {refillUserId === u.id && (
                <div className="col-span-12 flex items-center gap-2 py-1 pl-3 bg-lotus/5 rounded">
                  <span className="text-[10px] text-gray-400">Add credit to {u.username}:</span>
                  <input
                    type="number"
                    value={refillAmount}
                    onChange={(e) => setRefillAmount(e.target.value)}
                    placeholder="Amount"
                    className="h-6 w-24 px-2 text-[11px] bg-[var(--bg-primary)] border border-gray-700 rounded text-white focus:outline-none focus:border-lotus"
                  />
                  {[1000, 5000, 10000].map((a) => (
                    <button key={a} onClick={() => setRefillAmount(String(a))}
                      className="text-[9px] px-1.5 py-0.5 bg-surface-light border border-gray-800 rounded text-gray-400 hover:text-white transition">
                      {a >= 1000 ? `${a/1000}K` : a}
                    </button>
                  ))}
                  <button
                    onClick={() => refillUser(u.id as number)}
                    disabled={refilling || !refillAmount}
                    className="text-[10px] px-3 py-1 bg-lotus text-white rounded font-medium hover:bg-lotus-light transition disabled:opacity-50"
                  >
                    {refilling ? "..." : "Transfer"}
                  </button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {!loading && filtered.length > 0 && (
        <Pagination
          currentPage={currentPage}
          totalPages={totalPages}
          onPageChange={setCurrentPage}
        />
      )}
    </div>
  );
}
