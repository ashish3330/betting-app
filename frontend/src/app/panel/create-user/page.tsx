"use client";

import { useToast } from "@/components/Toast";
import { useState } from "react";
import { useAuth } from "@/lib/auth";
import { api } from "@/lib/api";
import Select from "@/components/Select";

export default function CreateUserPage() {
  const { user: me } = useAuth();
  const { addToast } = useToast();
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("");
  const [creditLimit, setCreditLimit] = useState(0);
  const [commissionRate, setCommissionRate] = useState(2);
  const [loading, setLoading] = useState(false);
  const [created, setCreated] = useState<{ username: string; password: string; role: string } | null>(null);
  const [error, setError] = useState("");

  const myRole = me?.role || "";

  const allowedRoles: Record<string, { value: string; label: string }[]> = {
    superadmin: [
      { value: "admin", label: "Admin" },
      { value: "master", label: "Master" },
      { value: "agent", label: "Agent" },
      { value: "client", label: "Client" },
    ],
    admin: [
      { value: "master", label: "Master" },
      { value: "agent", label: "Agent" },
      { value: "client", label: "Client" },
    ],
    master: [
      { value: "agent", label: "Agent" },
      { value: "client", label: "Client" },
    ],
    agent: [
      { value: "client", label: "Client (Betting ID)" },
    ],
  };

  const roles = allowedRoles[myRole] || [];

  async function generatePassword() {
    try {
      const data = await api.request<{ password: string }>("/api/v1/panel/generate-password", { auth: true, method: "POST" });
      setPassword(data.password);
    } catch {
      setPassword(Math.random().toString(36).slice(-8));
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);

    try {
      const data = await api.request<{ user: { username: string }; password: string; message: string }>(
        "/api/v1/panel/create-user",
        {
          method: "POST",
          auth: true,
          body: JSON.stringify({
            username,
            email: email || `${username}@lotus.exchange`,
            password,
            role,
            credit_limit: creditLimit,
            commission_rate: commissionRate,
          }),
        }
      );
      setCreated({ username, password: data.password, role });
      setUsername("");
      setEmail("");
      setPassword("");
      setCreditLimit(0);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create user");
    } finally {
      setLoading(false);
    }
  }

  function copyCredentials() {
    if (!created) return;
    const text = `Lotus Exchange Login\nUsername: ${created.username}\nPassword: ${created.password}\nURL: ${window.location.origin}/login`;
    navigator.clipboard.writeText(text);
    addToast({ type: "success", title: "Credentials copied to clipboard!" });
  }

  function shareWhatsApp() {
    if (!created) return;
    const text = encodeURIComponent(
      `*Lotus Exchange Login*\nUsername: ${created.username}\nPassword: ${created.password}\nLogin: ${window.location.origin}/login\n\n_Please change your password after first login._`
    );
    window.open(`https://wa.me/?text=${text}`, "_blank");
  }

  return (
    <div className="max-w-lg space-y-6">
      <div>
        <h1 className="text-xl font-bold text-white">Create User</h1>
        <p className="text-sm text-gray-500">
          Create a new {roles.length === 1 ? roles[0].label.toLowerCase() : "user"} under your account
        </p>
      </div>

      {/* Created Success Modal */}
      {created && (
        <div className="bg-profit/10 border border-profit/30 rounded-lg p-4 space-y-3">
          <div className="text-profit font-semibold text-sm">User Created Successfully!</div>
          <div className="bg-[var(--bg-primary)] rounded-lg p-3 font-mono text-sm space-y-1">
            <div><span className="text-gray-500">Username:</span> <span className="text-white">{created.username}</span></div>
            <div><span className="text-gray-500">Password:</span> <span className="text-white">{created.password}</span></div>
            <div><span className="text-gray-500">Role:</span> <span className="text-white capitalize">{created.role}</span></div>
            <div><span className="text-gray-500">Login URL:</span> <span className="text-blue-400">{typeof window !== "undefined" ? window.location.origin : ""}/login</span></div>
          </div>
          <div className="flex gap-2">
            <button onClick={copyCredentials} className="flex-1 bg-surface hover:bg-surface-light border border-gray-800 text-white text-xs px-3 py-2 rounded-lg transition">
              📋 Copy Credentials
            </button>
            <button onClick={shareWhatsApp} className="flex-1 bg-green-600/20 hover:bg-green-600/30 border border-green-600/30 text-green-400 text-xs px-3 py-2 rounded-lg transition">
              📱 Share via WhatsApp
            </button>
          </div>
          <button onClick={() => setCreated(null)} className="text-xs text-gray-500 hover:text-white transition">
            Create another user
          </button>
        </div>
      )}

      {/* Create Form */}
      {!created && (
        <form onSubmit={handleSubmit} className="bg-surface rounded-lg border border-gray-800/60 p-4 space-y-4">
          {error && (
            <div className="bg-loss/10 border border-loss/30 text-loss text-xs px-3 py-2 rounded-lg">
              {error}
            </div>
          )}

          {/* Role */}
          <div>
            <label className="text-xs text-gray-400 mb-1 block">Role *</label>
            <Select
              value={role}
              onChange={setRole}
              placeholder="Select Role"
              options={roles.map((r) => ({ value: r.value, label: r.label }))}
            />
          </div>

          {/* Username */}
          <div>
            <label className="text-xs text-gray-400 mb-1 block">Username *</label>
            <input
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value.toLowerCase().replace(/[^a-z0-9_]/g, ""))}
              required
              minLength={3}
              maxLength={20}
              placeholder="e.g. player_001"
              className="w-full h-9 px-3 text-sm bg-[var(--bg-primary)] border border-gray-800 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus"
            />
          </div>

          {/* Email (optional) */}
          <div>
            <label className="text-xs text-gray-400 mb-1 block">Email (optional)</label>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="user@email.com"
              className="w-full h-9 px-3 text-sm bg-[var(--bg-primary)] border border-gray-800 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus"
            />
          </div>

          {/* Password */}
          <div>
            <label className="text-xs text-gray-400 mb-1 block">Password *</label>
            <div className="flex gap-2">
              <input
                type="text"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                minLength={6}
                placeholder="Min 6 characters"
                className="flex-1 h-9 px-3 text-sm bg-[var(--bg-primary)] border border-gray-800 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus font-mono"
              />
              <button
                type="button"
                onClick={generatePassword}
                className="h-9 px-3 bg-surface-light hover:bg-surface-lighter border border-gray-800 text-xs text-gray-300 rounded-lg transition"
              >
                Generate
              </button>
            </div>
          </div>

          {/* Credit Limit */}
          <div>
            <label className="text-xs text-gray-400 mb-1 block">Credit Limit (₹)</label>
            <input
              type="number"
              value={creditLimit || ""}
              onChange={(e) => setCreditLimit(parseFloat(e.target.value) || 0)}
              min={0}
              placeholder="0"
              className="w-full h-9 px-3 text-sm bg-[var(--bg-primary)] border border-gray-800 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus font-mono"
            />
          </div>

          {/* Commission Rate */}
          <div>
            <label className="text-xs text-gray-400 mb-1 block">Commission Rate (%)</label>
            <input
              type="number"
              value={commissionRate}
              onChange={(e) => setCommissionRate(parseFloat(e.target.value) || 0)}
              min={0}
              max={100}
              step={0.5}
              className="w-full h-9 px-3 text-sm bg-[var(--bg-primary)] border border-gray-800 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:border-lotus font-mono"
            />
          </div>

          <button
            type="submit"
            disabled={loading || !username || !password || !role}
            className="w-full h-10 bg-lotus hover:bg-lotus-light disabled:bg-gray-700 disabled:text-gray-500 text-white text-sm font-bold rounded-lg transition"
          >
            {loading ? "Creating..." : `Create ${role || "User"}`}
          </button>
        </form>
      )}
    </div>
  );
}
