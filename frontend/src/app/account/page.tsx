"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { useAuth } from "@/lib/auth";
import { api } from "@/lib/api";
import Link from "next/link";

interface ResponsibleLimits {
  daily_deposit_limit: number;
  daily_loss_limit: number;
  session_limit_minutes: number;
  self_excluded: boolean;
  excluded_until?: string;
}

interface KYCDocument {
  name: string;
  data: string; // base64
  status: "pending" | "verified" | "rejected";
  uploadedAt: string;
}

export default function AccountPage() {
  const { user, isLoggedIn } = useAuth();

  if (!isLoggedIn) {
    return (
      <div className="max-w-7xl mx-auto px-3 py-16 text-center">
        <h2 className="text-lg font-bold text-white">Please Login</h2>
        <p className="text-sm text-gray-500 mt-1">
          You need to be logged in to view account settings.
        </p>
        <Link
          href="/login"
          className="inline-block mt-4 bg-lotus hover:bg-lotus-light text-white px-6 py-2 rounded-lg text-sm font-medium transition"
        >
          Login
        </Link>
      </div>
    );
  }

  return (
    <div className="max-w-3xl mx-auto px-3 py-4 space-y-6">
      <div>
        <h1 className="text-lg font-bold text-white">Account Settings</h1>
        <p className="text-xs text-gray-500">Manage your profile and preferences</p>
      </div>

      {/* Quick Links */}
      <div className="grid grid-cols-3 gap-2">
        <Link
          href="/account/deposit"
          className="bg-surface rounded-xl border border-gray-800 p-4 text-center hover:border-gray-700 transition group"
        >
          <div className="text-xl font-bold text-profit group-hover:scale-110 transition">+</div>
          <div className="text-xs text-gray-400 mt-1">Deposit</div>
        </Link>
        <Link
          href="/account/withdraw"
          className="bg-surface rounded-xl border border-gray-800 p-4 text-center hover:border-gray-700 transition group"
        >
          <div className="text-xl font-bold text-loss group-hover:scale-110 transition">-</div>
          <div className="text-xs text-gray-400 mt-1">Withdraw</div>
        </Link>
        <Link
          href="/account/history"
          className="bg-surface rounded-xl border border-gray-800 p-4 text-center hover:border-gray-700 transition group"
        >
          <div className="text-xl font-bold text-lotus group-hover:scale-110 transition">H</div>
          <div className="text-xs text-gray-400 mt-1">History</div>
        </Link>
      </div>

      {/* Profile Info */}
      <section className="bg-surface rounded-xl border border-gray-800 p-5">
        <h2 className="text-sm font-bold text-white mb-4">Profile Information</h2>
        <div className="space-y-3">
          <InfoRow label="Username" value={user?.username || "-"} />
          <InfoRow label="Email" value={user?.email || "-"} />
          <InfoRow label="Role" value={user?.role || "user"} />
          <InfoRow label="User ID" value={String(user?.id ?? "-")} />
        </div>
      </section>

      {/* Change Password */}
      <ChangePasswordSection />

      {/* KYC Upload */}
      <KYCUploadSection />

      {/* Responsible Gambling */}
      <ResponsibleGamblingSection />
    </div>
  );
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between py-2 border-b border-gray-800 last:border-0">
      <span className="text-xs text-gray-500">{label}</span>
      <span className="text-sm text-white font-mono">{value}</span>
    </div>
  );
}

function ChangePasswordSection() {
  const [oldPassword, setOldPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [status, setStatus] = useState<"idle" | "loading" | "success" | "error">("idle");
  const [message, setMessage] = useState("");

  async function handleChangePassword(e: React.FormEvent) {
    e.preventDefault();
    if (newPassword !== confirmPassword) {
      setStatus("error");
      setMessage("New passwords do not match");
      return;
    }
    if (newPassword.length < 6) {
      setStatus("error");
      setMessage("Password must be at least 6 characters");
      return;
    }
    setStatus("loading");
    try {
      await api.changePassword(oldPassword, newPassword);
      setStatus("success");
      setMessage("Password changed successfully");
      setOldPassword("");
      setNewPassword("");
      setConfirmPassword("");
    } catch {
      setStatus("error");
      setMessage("Failed to change password. Check your current password.");
    }
  }

  return (
    <section className="bg-surface rounded-xl border border-gray-800 p-5">
      <h2 className="text-sm font-bold text-white mb-4">Change Password</h2>
      <form onSubmit={handleChangePassword} className="space-y-3">
        <div>
          <label className="text-xs text-gray-400">Current Password</label>
          <input
            type="password"
            value={oldPassword}
            onChange={(e) => setOldPassword(e.target.value)}
            className="w-full mt-1 bg-surface-light border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-lotus"
            required
          />
        </div>
        <div>
          <label className="text-xs text-gray-400">New Password</label>
          <input
            type="password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            className="w-full mt-1 bg-surface-light border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-lotus"
            required
          />
        </div>
        <div>
          <label className="text-xs text-gray-400">Confirm New Password</label>
          <input
            type="password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            className="w-full mt-1 bg-surface-light border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-lotus"
            required
          />
        </div>
        {message && (
          <p className={`text-xs ${status === "success" ? "text-profit" : "text-loss"}`}>
            {message}
          </p>
        )}
        <button
          type="submit"
          disabled={status === "loading"}
          className="bg-lotus hover:bg-lotus-light text-white text-xs px-4 py-2 rounded-lg transition disabled:opacity-50"
        >
          {status === "loading" ? "Changing..." : "Change Password"}
        </button>
      </form>
    </section>
  );
}

// ========== KYC Upload Section ==========

const KYC_DOC_TYPES = [
  { key: "pan_card", label: "PAN Card" },
  { key: "aadhaar_card", label: "Aadhaar Card" },
  { key: "bank_statement", label: "Bank Statement" },
] as const;

function KYCUploadSection() {
  const [docs, setDocs] = useState<Record<string, KYCDocument>>({});

  useEffect(() => {
    // Load from localStorage
    const stored = localStorage.getItem("kyc_documents");
    if (stored) {
      try {
        setDocs(JSON.parse(stored));
      } catch {
        // ignore
      }
    }
  }, []);

  function handleFileUpload(docKey: string, docLabel: string, file: File) {
    const reader = new FileReader();
    reader.onload = () => {
      const base64 = reader.result as string;
      const newDocs = {
        ...docs,
        [docKey]: {
          name: file.name,
          data: base64,
          status: "pending" as const,
          uploadedAt: new Date().toISOString(),
        },
      };
      setDocs(newDocs);
      localStorage.setItem("kyc_documents", JSON.stringify(newDocs));
    };
    reader.readAsDataURL(file);
  }

  function getStatusBadge(status: string) {
    switch (status) {
      case "verified":
        return <span className="text-[10px] px-2 py-0.5 rounded bg-profit/20 text-profit font-medium">Verified</span>;
      case "rejected":
        return <span className="text-[10px] px-2 py-0.5 rounded bg-loss/20 text-loss font-medium">Rejected</span>;
      default:
        return <span className="text-[10px] px-2 py-0.5 rounded bg-yellow-500/20 text-yellow-500 font-medium">Pending</span>;
    }
  }

  const allUploaded = KYC_DOC_TYPES.every((dt) => docs[dt.key]);
  const allVerified = KYC_DOC_TYPES.every((dt) => docs[dt.key]?.status === "verified");

  return (
    <section className="bg-surface rounded-xl border border-gray-800 p-5">
      <h2 className="text-sm font-bold text-white mb-4">KYC Verification</h2>

      {/* Overall Status */}
      <div className="flex items-center gap-3 mb-4">
        <div
          className={`w-10 h-10 rounded-full flex items-center justify-center ${
            allVerified
              ? "bg-profit/20"
              : "bg-yellow-500/20"
          }`}
        >
          {allVerified ? (
            <svg className="w-5 h-5 text-profit" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
            </svg>
          ) : (
            <svg className="w-5 h-5 text-yellow-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01M12 2a10 10 0 100 20 10 10 0 000-20z" />
            </svg>
          )}
        </div>
        <div>
          <p className={`text-sm font-medium ${allVerified ? "text-profit" : "text-yellow-500"}`}>
            {allVerified ? "KYC Verified" : allUploaded ? "Under Review" : "Pending Verification"}
          </p>
          <p className="text-xs text-gray-500">
            {allVerified
              ? "Your identity has been verified successfully."
              : "Upload your ID and address proof to complete KYC."}
          </p>
        </div>
      </div>

      {/* Document Upload Cards */}
      <div className="space-y-3">
        {KYC_DOC_TYPES.map((docType) => {
          const doc = docs[docType.key];
          return (
            <KYCDocCard
              key={docType.key}
              docKey={docType.key}
              label={docType.label}
              doc={doc}
              onUpload={handleFileUpload}
              getStatusBadge={getStatusBadge}
            />
          );
        })}
      </div>
    </section>
  );
}

function KYCDocCard({
  docKey,
  label,
  doc,
  onUpload,
  getStatusBadge,
}: {
  docKey: string;
  label: string;
  doc?: KYCDocument;
  onUpload: (key: string, label: string, file: File) => void;
  getStatusBadge: (status: string) => React.ReactNode;
}) {
  const fileInputRef = useRef<HTMLInputElement>(null);

  return (
    <div className="bg-surface-light rounded-lg border border-gray-700 p-3 flex items-center justify-between">
      <div className="flex items-center gap-3 min-w-0">
        <div className="w-8 h-8 rounded bg-gray-700/50 flex items-center justify-center flex-shrink-0">
          <svg className="w-4 h-4 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
          </svg>
        </div>
        <div className="min-w-0">
          <p className="text-xs text-white font-medium">{label}</p>
          {doc ? (
            <div className="flex items-center gap-2 mt-0.5">
              <span className="text-[10px] text-gray-500 truncate max-w-[150px]">{doc.name}</span>
              {getStatusBadge(doc.status)}
            </div>
          ) : (
            <p className="text-[10px] text-gray-400">Not uploaded</p>
          )}
        </div>
      </div>
      <div>
        <input
          ref={fileInputRef}
          type="file"
          accept="image/*,.pdf"
          className="hidden"
          onChange={(e) => {
            const file = e.target.files?.[0];
            if (file) onUpload(docKey, label, file);
          }}
        />
        <button
          onClick={() => fileInputRef.current?.click()}
          className={`text-xs px-3 py-1.5 rounded-lg transition ${
            doc
              ? "bg-surface hover:bg-surface-lighter text-gray-400 border border-gray-700"
              : "bg-lotus hover:bg-lotus-light text-white"
          }`}
        >
          {doc ? "Re-upload" : "Upload"}
        </button>
      </div>
    </div>
  );
}

// ========== Responsible Gambling Section (wired to API) ==========

function ResponsibleGamblingSection() {
  const [limits, setLimits] = useState<ResponsibleLimits>({
    daily_deposit_limit: 0,
    daily_loss_limit: 0,
    session_limit_minutes: 0,
    self_excluded: false,
  });
  const [depositLimit, setDepositLimit] = useState("");
  const [lossLimit, setLossLimit] = useState("");
  const [sessionLimit, setSessionLimit] = useState("");
  const [saving, setSaving] = useState<string | null>(null);
  const [msg, setMsg] = useState<{ type: "success" | "error"; text: string } | null>(null);
  const [excluding, setExcluding] = useState(false);

  const fetchLimits = useCallback(async () => {
    try {
      const data = await api.getResponsibleLimits();
      setLimits(data);
      if (data.daily_deposit_limit > 0) setDepositLimit(String(data.daily_deposit_limit));
      if (data.daily_loss_limit > 0) setLossLimit(String(data.daily_loss_limit));
      if (data.session_limit_minutes > 0) setSessionLimit(String(data.session_limit_minutes));
    } catch {
      // API might not exist yet, use defaults
    }
  }, []);

  useEffect(() => {
    fetchLimits();
  }, [fetchLimits]);

  async function handleSetLimit(field: string, value: string) {
    const num = parseFloat(value);
    if (isNaN(num) || num < 0) {
      setMsg({ type: "error", text: "Please enter a valid positive number." });
      return;
    }
    setSaving(field);
    setMsg(null);
    try {
      const payload: Partial<ResponsibleLimits> = {};
      if (field === "deposit") payload.daily_deposit_limit = num;
      if (field === "loss") payload.daily_loss_limit = num;
      if (field === "session") payload.session_limit_minutes = num;
      await api.setResponsibleLimits(payload);
      setMsg({ type: "success", text: "Limit updated successfully." });
      fetchLimits();
    } catch {
      setMsg({ type: "error", text: "Failed to update limit." });
    } finally {
      setSaving(null);
    }
  }

  async function handleSelfExclude() {
    if (!confirm("Are you sure you want to self-exclude for 24 hours? You will not be able to place bets during this period.")) return;
    setExcluding(true);
    setMsg(null);
    try {
      await api.selfExclude();
      setMsg({ type: "success", text: "You have been self-excluded for 24 hours." });
      fetchLimits();
    } catch {
      setMsg({ type: "error", text: "Failed to self-exclude. Please try again." });
    } finally {
      setExcluding(false);
    }
  }

  return (
    <section className="bg-surface rounded-xl border border-gray-800 p-5">
      <h2 className="text-sm font-bold text-white mb-4">Responsible Gambling</h2>

      {limits.self_excluded && (
        <div className="bg-loss/10 border border-loss/30 rounded-lg p-3 mb-4">
          <p className="text-xs text-loss font-medium">
            You are currently self-excluded{limits.excluded_until ? ` until ${new Date(limits.excluded_until).toLocaleString()}` : ""}.
          </p>
        </div>
      )}

      {/* Current Limits Display */}
      {(limits.daily_deposit_limit > 0 || limits.daily_loss_limit > 0 || limits.session_limit_minutes > 0) && (
        <div className="bg-surface-light rounded-lg border border-gray-700 p-3 mb-4">
          <p className="text-[10px] text-gray-500 uppercase tracking-wider mb-2">Current Limits</p>
          <div className="flex flex-wrap gap-4">
            {limits.daily_deposit_limit > 0 && (
              <div>
                <span className="text-[10px] text-gray-500">Daily Deposit</span>
                <p className="text-sm text-white font-mono">{limits.daily_deposit_limit.toLocaleString()}</p>
              </div>
            )}
            {limits.daily_loss_limit > 0 && (
              <div>
                <span className="text-[10px] text-gray-500">Daily Loss</span>
                <p className="text-sm text-white font-mono">{limits.daily_loss_limit.toLocaleString()}</p>
              </div>
            )}
            {limits.session_limit_minutes > 0 && (
              <div>
                <span className="text-[10px] text-gray-500">Session</span>
                <p className="text-sm text-white font-mono">{limits.session_limit_minutes} min</p>
              </div>
            )}
          </div>
        </div>
      )}

      <div className="space-y-4">
        <div>
          <label className="text-xs text-gray-400">Daily Deposit Limit</label>
          <div className="flex gap-2 mt-1">
            <input
              type="number"
              placeholder="Enter amount"
              value={depositLimit}
              onChange={(e) => setDepositLimit(e.target.value)}
              className="flex-1 bg-surface-light border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-lotus"
            />
            <button
              onClick={() => handleSetLimit("deposit", depositLimit)}
              disabled={saving === "deposit"}
              className="bg-lotus hover:bg-lotus-light text-white text-xs px-4 py-2 rounded-lg transition disabled:opacity-50"
            >
              {saving === "deposit" ? "..." : "Set"}
            </button>
          </div>
        </div>
        <div>
          <label className="text-xs text-gray-400">Daily Loss Limit</label>
          <div className="flex gap-2 mt-1">
            <input
              type="number"
              placeholder="Enter amount"
              value={lossLimit}
              onChange={(e) => setLossLimit(e.target.value)}
              className="flex-1 bg-surface-light border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-lotus"
            />
            <button
              onClick={() => handleSetLimit("loss", lossLimit)}
              disabled={saving === "loss"}
              className="bg-lotus hover:bg-lotus-light text-white text-xs px-4 py-2 rounded-lg transition disabled:opacity-50"
            >
              {saving === "loss" ? "..." : "Set"}
            </button>
          </div>
        </div>
        <div>
          <label className="text-xs text-gray-400">Session Time Limit (minutes)</label>
          <div className="flex gap-2 mt-1">
            <input
              type="number"
              placeholder="e.g. 120"
              value={sessionLimit}
              onChange={(e) => setSessionLimit(e.target.value)}
              className="flex-1 bg-surface-light border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-lotus"
            />
            <button
              onClick={() => handleSetLimit("session", sessionLimit)}
              disabled={saving === "session"}
              className="bg-lotus hover:bg-lotus-light text-white text-xs px-4 py-2 rounded-lg transition disabled:opacity-50"
            >
              {saving === "session" ? "..." : "Set"}
            </button>
          </div>
        </div>

        {msg && (
          <p className={`text-xs ${msg.type === "success" ? "text-profit" : "text-loss"}`}>
            {msg.text}
          </p>
        )}

        <div className="pt-2 border-t border-gray-800 flex items-center justify-between">
          <button
            onClick={handleSelfExclude}
            disabled={excluding || limits.self_excluded}
            className="text-xs text-loss hover:text-red-400 transition disabled:opacity-50"
          >
            {excluding ? "Processing..." : limits.self_excluded ? "Already Self-Excluded" : "Self-Exclude for 24 Hours"}
          </button>
          <Link href="/responsible-gambling" className="text-xs text-lotus hover:underline">
            Learn More
          </Link>
        </div>
      </div>
    </section>
  );
}
