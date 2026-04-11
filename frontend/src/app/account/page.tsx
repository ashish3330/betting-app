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

type KYCOverallStatus = "verified" | "pending" | "not_submitted" | "rejected";

interface KYCStatusResponse {
  status?: KYCOverallStatus;
  overall_status?: KYCOverallStatus;
  documents?: Array<{ type: string; status: string }>;
}

function fmtINR(n: number | null | undefined) {
  if (n === null || n === undefined || Number.isNaN(n)) return "\u20B90.00";
  return "\u20B9" + n.toLocaleString("en-IN", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function todayISO() {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  return d.toISOString().slice(0, 10);
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

      {/* KPI strip */}
      <AccountKPIs />

      {/* Quick Links */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
        <QuickLink href="/wallet" label="Deposit" accent="text-profit">
          <svg className="w-5 h-5" fill="none" stroke="currentColor" strokeWidth={1.5} viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" d="M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5M16.5 12 12 16.5m0 0L7.5 12M12 16.5V3" />
          </svg>
        </QuickLink>
        <QuickLink href="/wallet" label="Withdraw" accent="text-loss">
          <svg className="w-5 h-5" fill="none" stroke="currentColor" strokeWidth={1.5} viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" d="M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5M7.5 7.5 12 3m0 0 4.5 4.5M12 3v13.5" />
          </svg>
        </QuickLink>
        <QuickLink href="/account/history" label="History" accent="text-lotus">
          <svg className="w-5 h-5" fill="none" stroke="currentColor" strokeWidth={1.5} viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />
          </svg>
        </QuickLink>
        <QuickLink href="/account/referral" label="Referral" accent="text-amber-400">
          <svg className="w-5 h-5" fill="none" stroke="currentColor" strokeWidth={1.5} viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" d="M21 11.25v8.25a1.5 1.5 0 0 1-1.5 1.5H5.25a1.5 1.5 0 0 1-1.5-1.5v-8.25M12 4.875A3.375 3.375 0 1 0 15.375 8.25H12V4.875ZM12 4.875A3.375 3.375 0 1 1 8.625 8.25H12V4.875ZM12 4.875v14.25m-9-9h18" />
          </svg>
        </QuickLink>
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

function QuickLink({ href, label, accent, children }: { href: string; label: string; accent: string; children: React.ReactNode }) {
  return (
    <Link
      href={href}
      className="bg-surface rounded-xl border border-gray-800 p-3 sm:p-4 flex flex-col items-center gap-1.5 hover:border-gray-700 hover:bg-surface-light transition group"
    >
      <div className={`${accent} group-hover:scale-110 transition`}>{children}</div>
      <div className="text-xs text-gray-400 group-hover:text-white transition">{label}</div>
    </Link>
  );
}

function AccountKPIs() {
  const [balance, setBalance] = useState<number | null>(null);
  const [available, setAvailable] = useState<number | null>(null);
  const [exposure, setExposure] = useState<number | null>(null);
  const [openBets, setOpenBets] = useState<number | null>(null);
  const [todayPnL, setTodayPnL] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const [bal, openHist, todayHist] = await Promise.allSettled([
          api.getBalance(),
          api.fetchBettingHistory({ status: "open" }),
          api.fetchBettingHistory({ status: "settled", from: todayISO() }),
        ]);
        if (cancelled) return;

        if (bal.status === "fulfilled") {
          setBalance(bal.value.balance ?? 0);
          setAvailable(bal.value.available_balance ?? bal.value.balance ?? 0);
          setExposure(bal.value.exposure ?? 0);
        }
        if (openHist.status === "fulfilled") {
          const bets = openHist.value?.bets || [];
          setOpenBets(bets.length);
        }
        if (todayHist.status === "fulfilled") {
          const bets = todayHist.value?.bets || [];
          const start = new Date();
          start.setHours(0, 0, 0, 0);
          const sum = bets.reduce((acc, b) => {
            const settledAt = b.settled_at ? new Date(b.settled_at) : null;
            if (!settledAt || settledAt < start) return acc;
            return acc + (b.pnl ?? 0);
          }, 0);
          setTodayPnL(sum);
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const cards = [
    {
      label: "Available",
      value: fmtINR(available ?? balance ?? 0),
      accent: "text-white",
      icon: (
        <svg className="w-5 h-5 text-gray-400" fill="none" stroke="currentColor" strokeWidth={1.5} viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" d="M2.25 8.25h19.5M2.25 9h19.5m-16.5 5.25h6m-6 2.25h3m-3.75 3h15a2.25 2.25 0 0 0 2.25-2.25V6.75A2.25 2.25 0 0 0 19.5 4.5h-15a2.25 2.25 0 0 0-2.25 2.25v10.5A2.25 2.25 0 0 0 4.5 19.5Z" />
        </svg>
      ),
    },
    {
      label: "Exposure",
      value: fmtINR(exposure ?? 0),
      accent: "text-loss",
      icon: (
        <svg className="w-5 h-5 text-loss/80" fill="none" stroke="currentColor" strokeWidth={1.5} viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m9-.75a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9 3.75h.008v.008H12v-.008Z" />
        </svg>
      ),
    },
    {
      label: "Open Bets",
      value: openBets === null ? "-" : String(openBets),
      accent: "text-white",
      icon: (
        <svg className="w-5 h-5 text-gray-400" fill="none" stroke="currentColor" strokeWidth={1.5} viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" d="M16.5 6v.75m0 3v.75m0 3v.75m0 3V18m-9-5.25h5.25M7.5 15h3M3.375 5.25c-.621 0-1.125.504-1.125 1.125v3.026a2.999 2.999 0 0 1 0 5.198v3.026c0 .621.504 1.125 1.125 1.125h17.25c.621 0 1.125-.504 1.125-1.125v-3.026a2.999 2.999 0 0 1 0-5.198V6.375c0-.621-.504-1.125-1.125-1.125H3.375Z" />
        </svg>
      ),
    },
    {
      label: "Today's P&L",
      value: (todayPnL ?? 0) >= 0 ? `+${fmtINR(todayPnL ?? 0)}` : `-${fmtINR(Math.abs(todayPnL ?? 0))}`,
      accent: (todayPnL ?? 0) >= 0 ? "text-profit" : "text-loss",
      icon: (
        <svg className="w-5 h-5 text-gray-400" fill="none" stroke="currentColor" strokeWidth={1.5} viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" d="M2.25 18 9 11.25l4.306 4.306a11.95 11.95 0 0 1 5.814-5.518l2.74-1.22m0 0-5.94-2.281m5.94 2.28-2.28 5.941" />
        </svg>
      ),
    },
  ];

  return (
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
      {cards.map((c) => (
        <div key={c.label} className="bg-surface rounded-xl border border-gray-800 p-3">
          <div className="flex items-center gap-2">
            {c.icon}
            <span className="text-[10px] uppercase tracking-wider text-gray-500">{c.label}</span>
          </div>
          <div className={`mt-2 text-sm font-mono font-semibold ${c.accent}`}>
            {loading ? <span className="inline-block w-16 h-4 bg-gray-800 animate-pulse rounded" /> : c.value}
          </div>
        </div>
      ))}
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

const ALLOWED_FILE_TYPES = ["image/jpeg", "image/png", "application/pdf"];
const MAX_FILE_SIZE = 5 * 1024 * 1024; // 5MB

interface KYCDocState {
  name: string;
  status: "pending" | "verified" | "rejected";
  uploadedAt: string;
}

function KYCUploadSection() {
  const [docs, setDocs] = useState<Record<string, KYCDocState>>({});
  const [overallStatus, setOverallStatus] = useState<KYCOverallStatus>("not_submitted");
  const [statusLoaded, setStatusLoaded] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [uploading, setUploading] = useState<string | null>(null);

  useEffect(() => {
    // Clean up any legacy localStorage data.
    try {
      localStorage.removeItem("kyc_documents");
    } catch {
      /* ignore */
    }

    // Fetch real KYC state from the backend so the UI survives reloads.
    (async () => {
      try {
        const data = await api.request<KYCStatusResponse>("/api/v1/kyc/status", { auth: true });
        const status = (data.overall_status || data.status || "not_submitted") as KYCOverallStatus;
        setOverallStatus(status);
        if (Array.isArray(data.documents)) {
          const mapped: Record<string, KYCDocState> = {};
          for (const d of data.documents) {
            mapped[d.type] = {
              name: d.type,
              status: (d.status === "verified" || d.status === "rejected" ? d.status : "pending") as KYCDocState["status"],
              uploadedAt: "",
            };
          }
          setDocs(mapped);
        }
      } catch {
        // Endpoint may not exist yet — fall back to "not_submitted".
      } finally {
        setStatusLoaded(true);
      }
    })();
  }, []);

  async function handleFileUpload(docKey: string, docLabel: string, file: File) {
    setUploadError(null);

    // Validate file type
    if (!ALLOWED_FILE_TYPES.includes(file.type)) {
      setUploadError("Only JPEG, PNG, and PDF files are allowed.");
      return;
    }

    // Validate file size
    if (file.size > MAX_FILE_SIZE) {
      setUploadError("File size must not exceed 5MB.");
      return;
    }

    setUploading(docKey);
    try {
      const formData = new FormData();
      formData.append("file", file);
      formData.append("document_type", docKey);

      // Auth is carried by the HttpOnly access_token cookie set at login.
      const res = await fetch("/api/v1/kyc/upload", {
        method: "POST",
        credentials: "include",
        body: formData,
      });

      if (!res.ok) {
        const errData = await res.json().catch(() => ({}));
        throw new Error(errData.error || `Upload failed (${res.status})`);
      }

      setDocs((prev) => ({
        ...prev,
        [docKey]: {
          name: file.name,
          status: "pending",
          uploadedAt: new Date().toISOString(),
        },
      }));
    } catch (err) {
      setUploadError(err instanceof Error ? err.message : "Upload failed. Please try again.");
    } finally {
      setUploading(null);
    }
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
  const allVerified = overallStatus === "verified" || KYC_DOC_TYPES.every((dt) => docs[dt.key]?.status === "verified");
  const isRejected = overallStatus === "rejected";

  const statusLabel: { label: string; hint: string; color: string; dot: string } = allVerified
    ? { label: "Verified", hint: "Your identity has been verified successfully.", color: "text-profit", dot: "bg-profit/20" }
    : isRejected
    ? { label: "Rejected", hint: "One or more documents were rejected. Please re-upload.", color: "text-loss", dot: "bg-loss/20" }
    : allUploaded || overallStatus === "pending"
    ? { label: "Under Review", hint: "Your documents are being reviewed by our team.", color: "text-yellow-500", dot: "bg-yellow-500/20" }
    : { label: "Not Submitted", hint: "Upload your ID and address proof to complete KYC.", color: "text-gray-400", dot: "bg-gray-700/40" };

  return (
    <section className="bg-surface rounded-xl border border-gray-800 p-5">
      <h2 className="text-sm font-bold text-white mb-4">KYC Verification</h2>

      {/* Overall Status */}
      <div className="flex items-center gap-3 mb-4">
        <div className={`w-10 h-10 rounded-full flex items-center justify-center ${statusLabel.dot}`}>
          {allVerified ? (
            <svg className="w-5 h-5 text-profit" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
            </svg>
          ) : isRejected ? (
            <svg className="w-5 h-5 text-loss" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          ) : (
            <svg className="w-5 h-5 text-yellow-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01M12 2a10 10 0 100 20 10 10 0 000-20z" />
            </svg>
          )}
        </div>
        <div>
          <p className={`text-sm font-medium ${statusLabel.color}`}>
            {statusLoaded ? `KYC Status: ${statusLabel.label}` : "Loading..."}
          </p>
          <p className="text-xs text-gray-500">{statusLabel.hint}</p>
        </div>
      </div>

      {/* Upload Error */}
      {uploadError && (
        <div className="bg-loss/10 border border-loss/30 rounded-lg p-3 mb-3">
          <p className="text-xs text-loss">{uploadError}</p>
        </div>
      )}

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
              isUploading={uploading === docType.key}
            />
          );
        })}
      </div>

      <p className="text-[10px] text-gray-500 mt-2">Accepted: JPEG, PNG, PDF. Max 5MB per file.</p>
    </section>
  );
}

function KYCDocCard({
  docKey,
  label,
  doc,
  onUpload,
  getStatusBadge,
  isUploading,
}: {
  docKey: string;
  label: string;
  doc?: KYCDocState;
  onUpload: (key: string, label: string, file: File) => void;
  getStatusBadge: (status: string) => React.ReactNode;
  isUploading?: boolean;
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
          accept=".jpeg,.jpg,.png,.pdf"
          className="hidden"
          onChange={(e) => {
            const file = e.target.files?.[0];
            if (file) onUpload(docKey, label, file);
          }}
        />
        <button
          onClick={() => fileInputRef.current?.click()}
          disabled={isUploading}
          className={`text-xs px-3 py-1.5 rounded-lg transition ${
            isUploading
              ? "bg-surface text-gray-500 cursor-not-allowed"
              : doc
              ? "bg-surface hover:bg-surface-lighter text-gray-400 border border-gray-700"
              : "bg-lotus hover:bg-lotus-light text-white"
          }`}
        >
          {isUploading ? "Uploading..." : doc ? "Re-upload" : "Upload"}
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
