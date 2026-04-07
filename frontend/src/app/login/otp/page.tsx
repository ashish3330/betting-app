"use client";

import { useState, useEffect, useRef } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { api } from "@/lib/api";
import { encryptLocalStorage } from "@/lib/crypto";

export default function OTPVerificationPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const userId = Number(searchParams.get("user_id") || "0");
  const mockCode = searchParams.get("code") || "";

  const [digits, setDigits] = useState(["", "", "", "", "", ""]);
  const [status, setStatus] = useState<"idle" | "loading" | "error">("idle");
  const [error, setError] = useState("");
  const [timeLeft, setTimeLeft] = useState(300); // 5 minutes
  const [resending, setResending] = useState(false);
  const inputRefs = useRef<(HTMLInputElement | null)[]>([]);

  // Countdown timer
  useEffect(() => {
    if (timeLeft <= 0) return;
    const timer = setInterval(() => setTimeLeft((t) => t - 1), 1000);
    return () => clearInterval(timer);
  }, [timeLeft]);

  // Focus first input on mount
  useEffect(() => {
    inputRefs.current[0]?.focus();
  }, []);

  if (!userId) {
    return (
      <div className="min-h-screen bg-[var(--bg-primary)] flex items-center justify-center px-4">
        <div className="text-center">
          <h2 className="text-lg font-bold text-white">Invalid Session</h2>
          <p className="text-sm text-gray-500 mt-2">
            No OTP session found. Please login again.
          </p>
          <button
            onClick={() => router.push("/login")}
            className="mt-4 bg-lotus hover:bg-lotus-light text-white px-6 py-2 rounded-lg text-sm font-medium transition"
          >
            Back to Login
          </button>
        </div>
      </div>
    );
  }

  function handleDigitChange(index: number, value: string) {
    if (!/^\d*$/.test(value)) return;
    const newDigits = [...digits];
    newDigits[index] = value.slice(-1);
    setDigits(newDigits);

    // Auto-advance to next input
    if (value && index < 5) {
      inputRefs.current[index + 1]?.focus();
    }
  }

  function handleKeyDown(index: number, e: React.KeyboardEvent) {
    if (e.key === "Backspace" && !digits[index] && index > 0) {
      inputRefs.current[index - 1]?.focus();
    }
  }

  function handlePaste(e: React.ClipboardEvent) {
    e.preventDefault();
    const pasted = e.clipboardData.getData("text").replace(/\D/g, "").slice(0, 6);
    const newDigits = [...digits];
    for (let i = 0; i < pasted.length; i++) {
      newDigits[i] = pasted[i];
    }
    setDigits(newDigits);
    if (pasted.length > 0) {
      const focusIndex = Math.min(pasted.length, 5);
      inputRefs.current[focusIndex]?.focus();
    }
  }

  async function handleVerify() {
    const code = digits.join("");
    if (code.length !== 6) {
      setError("Please enter all 6 digits");
      return;
    }

    setStatus("loading");
    setError("");

    try {
      const data = await api.completeOTPLogin(userId, code);
      // Redirect based on role
      const role = data.user?.role;
      if (
        role === "superadmin" ||
        role === "admin" ||
        role === "master" ||
        role === "agent"
      ) {
        router.push("/panel");
      } else {
        router.push("/");
      }
    } catch (err) {
      setStatus("error");
      setError(
        err instanceof Error ? err.message : "Invalid or expired OTP code"
      );
    }
  }

  async function handleResend() {
    setResending(true);
    try {
      // For mock, we re-trigger login flow; in production this would be a resend endpoint
      setTimeLeft(300);
      setDigits(["", "", "", "", "", ""]);
      setError("");
      inputRefs.current[0]?.focus();
    } finally {
      setResending(false);
    }
  }

  const formatTime = (s: number) => {
    const min = Math.floor(s / 60);
    const sec = s % 60;
    return `${min}:${sec.toString().padStart(2, "0")}`;
  };

  return (
    <div className="min-h-screen bg-[var(--bg-primary)] flex items-center justify-center px-4">
      <div className="w-full max-w-md">
        <div className="bg-surface rounded-2xl border border-gray-800 p-8">
          {/* Header */}
          <div className="text-center mb-8">
            <div className="w-16 h-16 mx-auto mb-4 bg-lotus/20 rounded-full flex items-center justify-center">
              <svg
                className="w-8 h-8 text-lotus"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={1.5}
                  d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"
                />
              </svg>
            </div>
            <h1 className="text-xl font-bold text-white">
              Two-Factor Authentication
            </h1>
            <p className="text-sm text-gray-500 mt-2">
              Enter the 6-digit code to verify your identity
            </p>
            {mockCode && (
              <p className="text-xs text-lotus mt-2 font-mono bg-lotus/10 rounded px-3 py-1 inline-block">
                Mock code: {mockCode}
              </p>
            )}
          </div>

          {/* OTP Input */}
          <div className="flex justify-center gap-3 mb-6" onPaste={handlePaste}>
            {digits.map((digit, i) => (
              <input
                key={i}
                ref={(el) => { inputRefs.current[i] = el; }}
                type="text"
                inputMode="numeric"
                maxLength={1}
                value={digit}
                onChange={(e) => handleDigitChange(i, e.target.value)}
                onKeyDown={(e) => handleKeyDown(i, e)}
                className="w-12 h-14 text-center text-xl font-bold text-white bg-surface-light border border-gray-700 rounded-xl focus:outline-none focus:border-lotus focus:ring-1 focus:ring-lotus transition"
              />
            ))}
          </div>

          {/* Timer */}
          <div className="text-center mb-6">
            {timeLeft > 0 ? (
              <p className="text-sm text-gray-500">
                Code expires in{" "}
                <span className="text-white font-mono">
                  {formatTime(timeLeft)}
                </span>
              </p>
            ) : (
              <p className="text-sm text-loss">Code has expired</p>
            )}
          </div>

          {/* Error */}
          {error && (
            <div className="mb-4 p-3 bg-loss/10 border border-loss/30 rounded-lg">
              <p className="text-xs text-loss text-center">{error}</p>
            </div>
          )}

          {/* Verify Button */}
          <button
            onClick={handleVerify}
            disabled={
              status === "loading" || digits.join("").length !== 6 || timeLeft <= 0
            }
            className="w-full bg-lotus hover:bg-lotus-light text-white font-medium py-3 rounded-xl transition disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {status === "loading" ? (
              <span className="flex items-center justify-center gap-2">
                <span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                Verifying...
              </span>
            ) : (
              "Verify"
            )}
          </button>

          {/* Resend */}
          <div className="text-center mt-4">
            <button
              onClick={handleResend}
              disabled={resending || timeLeft > 240}
              className="text-sm text-lotus hover:text-lotus-light transition disabled:opacity-40 disabled:cursor-not-allowed"
            >
              {resending ? "Resending..." : "Resend Code"}
            </button>
          </div>

          {/* Back to login */}
          <div className="text-center mt-4">
            <button
              onClick={() => router.push("/login")}
              className="text-xs text-gray-500 hover:text-gray-400 transition"
            >
              Back to Login
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
