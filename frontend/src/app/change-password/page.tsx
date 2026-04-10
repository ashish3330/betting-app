"use client";

import { useState, useEffect } from "react";
import { useAuth } from "@/lib/auth";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { api } from "@/lib/api";
import { getTheme, type Theme } from "@/lib/theme";

export default function ChangePasswordPage() {
  const { isLoggedIn, isLoading } = useAuth();
  const router = useRouter();

  const [oldPassword, setOldPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [showOld, setShowOld] = useState(false);
  const [showNew, setShowNew] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [loading, setLoading] = useState(false);
  const [theme, setTheme] = useState<Theme>("dark");

  useEffect(() => {
    setTheme(getTheme());
  }, []);

  useEffect(() => {
    if (!isLoading && !isLoggedIn) {
      router.push("/login");
    }
  }, [isLoading, isLoggedIn, router]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setSuccess("");

    if (!oldPassword || !newPassword || !confirmPassword) {
      setError("Please fill all fields");
      return;
    }
    if (newPassword !== confirmPassword) {
      setError("New passwords do not match");
      return;
    }
    if (newPassword.length < 6) {
      setError("New password must be at least 6 characters");
      return;
    }
    if (oldPassword === newPassword) {
      setError("New password must be different from the current password");
      return;
    }

    setLoading(true);
    try {
      await api.changePassword(oldPassword, newPassword);
      setSuccess("Password changed successfully. Redirecting...");
      setOldPassword("");
      setNewPassword("");
      setConfirmPassword("");
      setTimeout(() => router.push("/account"), 1500);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to change password");
    } finally {
      setLoading(false);
    }
  };

  if (isLoading) return null;
  if (!isLoggedIn) return null;

  return (
    <div className="min-h-[calc(100vh-100px)] flex items-center justify-center px-4">
      <div className="w-full max-w-sm">
        {/* Logo */}
        <div className="text-center mb-6">
          <img
            src={theme === "dark" ? "/logo.svg?v=3" : "/logo-light.svg?v=3"}
            alt="3XBet"
            className="h-16 w-auto mx-auto mb-3"
          />
          <h1 className="text-lg font-bold text-white">Change Password</h1>
          <p className="text-xs text-gray-400 mt-1">Update your account password</p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-3">
          <div>
            <label className="text-[11px] text-gray-400 block mb-1">Current Password</label>
            <div className="relative">
              <input
                type={showOld ? "text" : "password"}
                value={oldPassword}
                onChange={(e) => setOldPassword(e.target.value)}
                placeholder="Enter current password"
                autoComplete="current-password"
                className="w-full h-10 px-3 pr-10 bg-surface border border-gray-700/60 rounded-lg text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-lotus/60 transition"
              />
              <button
                type="button"
                onClick={() => setShowOld(!showOld)}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 text-xs"
              >
                {showOld ? "Hide" : "Show"}
              </button>
            </div>
          </div>

          <div>
            <label className="text-[11px] text-gray-400 block mb-1">New Password</label>
            <div className="relative">
              <input
                type={showNew ? "text" : "password"}
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                placeholder="Enter new password"
                autoComplete="new-password"
                className="w-full h-10 px-3 pr-10 bg-surface border border-gray-700/60 rounded-lg text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-lotus/60 transition"
              />
              <button
                type="button"
                onClick={() => setShowNew(!showNew)}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 text-xs"
              >
                {showNew ? "Hide" : "Show"}
              </button>
            </div>
          </div>

          <div>
            <label className="text-[11px] text-gray-400 block mb-1">Confirm New Password</label>
            <input
              type={showNew ? "text" : "password"}
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              placeholder="Re-enter new password"
              autoComplete="new-password"
              className="w-full h-10 px-3 bg-surface border border-gray-700/60 rounded-lg text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-lotus/60 transition"
            />
          </div>

          {error && (
            <div className="text-xs text-loss bg-loss/10 border border-loss/20 rounded-lg px-3 py-2">
              {error}
            </div>
          )}
          {success && (
            <div className="text-xs text-profit bg-profit/10 border border-profit/20 rounded-lg px-3 py-2">
              {success}
            </div>
          )}

          <button
            type="submit"
            disabled={loading}
            className="w-full h-10 bg-lotus hover:bg-lotus-light text-white rounded-lg text-sm font-semibold transition disabled:opacity-50"
          >
            {loading ? "Updating..." : "Change Password"}
          </button>
        </form>

        <p className="text-center text-xs text-gray-500 mt-4">
          <Link href="/account" className="text-lotus hover:underline">
            Back to Account
          </Link>
        </p>
      </div>
    </div>
  );
}
