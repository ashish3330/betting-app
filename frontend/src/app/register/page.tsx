"use client";

import { useState, useMemo } from "react";
import { api } from "@/lib/api";
import { useRouter } from "next/navigation";
import Link from "next/link";

export default function RegisterPage() {
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [mobile, setMobile] = useState("");
  const [dob, setDob] = useState("");
  const [referralCode, setReferralCode] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [acceptedTerms, setAcceptedTerms] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const router = useRouter();

  // Live password strength checks
  const hasMinLength = password.length >= 8;
  const hasUpper = /[A-Z]/.test(password);
  const hasLower = /[a-z]/.test(password);
  const hasNumber = /[0-9]/.test(password);
  const passwordValid = hasMinLength && hasUpper && hasLower && hasNumber;

  // Age (18+) check from DOB
  const age = useMemo(() => {
    if (!dob) return null;
    const birth = new Date(dob);
    if (isNaN(birth.getTime())) return null;
    const today = new Date();
    let a = today.getFullYear() - birth.getFullYear();
    const m = today.getMonth() - birth.getMonth();
    if (m < 0 || (m === 0 && today.getDate() < birth.getDate())) a--;
    return a;
  }, [dob]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!username || !email || !mobile || !dob || !password) {
      setError("All required fields must be filled");
      return;
    }
    if (password !== confirmPassword) {
      setError("Passwords do not match");
      return;
    }
    // Username validation: 3-30 chars, alphanumeric + ._-
    const usernameRegex = /^[a-zA-Z0-9._-]{3,30}$/;
    if (!usernameRegex.test(username)) {
      setError("Username must be 3-30 characters (letters, numbers, dots, underscores, hyphens)");
      return;
    }
    // Mobile validation: 10 digits
    const mobileDigits = mobile.replace(/\D/g, "");
    if (mobileDigits.length < 10) {
      setError("Please enter a valid 10-digit mobile number");
      return;
    }
    if (!passwordValid) {
      setError("Password does not meet all requirements");
      return;
    }
    if (age === null || age < 18) {
      setError("You must be at least 18 years old to register");
      return;
    }
    if (!acceptedTerms) {
      setError("Please accept the Terms & Conditions to continue");
      return;
    }

    setLoading(true);
    setError("");

    try {
      await api.register(username, email, password, "user", dob);
      router.push("/login?registered=1");
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Registration failed. Try again."
      );
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-[80vh] flex items-center justify-center px-4 py-8">
      <div className="w-full max-w-sm">
        <div className="text-center mb-6">
          <img
            src="/logo.svg?v=3"
            alt="Lotus Exchange"
            className="h-14 w-auto mx-auto mb-3"
          />
          <h1 className="text-lg font-bold text-white">Create your account</h1>
          <p className="text-xs text-gray-400 mt-1">
            Join Lotus Exchange today
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-3">
          <div>
            <label className="text-[11px] text-gray-400 block mb-1">
              Username <span className="text-loss">*</span>
            </label>
            <input
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="Choose a username"
              className="w-full h-10 px-3 bg-surface border border-gray-700/60 rounded-lg text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-lotus/60 transition"
              autoComplete="username"
            />
          </div>

          <div>
            <label className="text-[11px] text-gray-400 block mb-1">
              Email <span className="text-loss">*</span>
            </label>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@example.com"
              className="w-full h-10 px-3 bg-surface border border-gray-700/60 rounded-lg text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-lotus/60 transition"
              autoComplete="email"
            />
          </div>

          <div>
            <label className="text-[11px] text-gray-400 block mb-1">
              Mobile Number <span className="text-loss">*</span>
            </label>
            <div className="flex gap-2">
              <span className="h-10 px-3 flex items-center bg-surface-light border border-gray-700/60 rounded-lg text-sm text-gray-300 font-mono">
                +91
              </span>
              <input
                type="tel"
                value={mobile}
                onChange={(e) => setMobile(e.target.value.replace(/\D/g, "").slice(0, 10))}
                placeholder="10-digit mobile"
                inputMode="numeric"
                className="flex-1 h-10 px-3 bg-surface border border-gray-700/60 rounded-lg text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-lotus/60 transition tabular-nums"
                autoComplete="tel-national"
              />
            </div>
          </div>

          <div>
            <label className="text-[11px] text-gray-400 block mb-1">
              Date of Birth <span className="text-loss">*</span>
            </label>
            <input
              type="date"
              value={dob}
              onChange={(e) => setDob(e.target.value)}
              max={new Date().toISOString().split("T")[0]}
              className="w-full h-10 px-3 bg-surface border border-gray-700/60 rounded-lg text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-lotus/60 transition"
            />
            {age !== null && age < 18 && (
              <p className="text-[10px] text-loss mt-1">You must be 18 or older</p>
            )}
          </div>

          <div>
            <label className="text-[11px] text-gray-400 block mb-1">
              Password <span className="text-loss">*</span>
            </label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="Create a strong password"
              className="w-full h-10 px-3 bg-surface border border-gray-700/60 rounded-lg text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-lotus/60 transition"
              autoComplete="new-password"
            />
            <ul className="text-[10px] space-y-0.5 mt-1.5">
              <li className={hasMinLength ? "text-green-400" : "text-gray-500"}>
                {hasMinLength ? "\u2713" : "\u00b7"} At least 8 characters
              </li>
              <li className={hasUpper ? "text-green-400" : "text-gray-500"}>
                {hasUpper ? "\u2713" : "\u00b7"} One uppercase letter
              </li>
              <li className={hasLower ? "text-green-400" : "text-gray-500"}>
                {hasLower ? "\u2713" : "\u00b7"} One lowercase letter
              </li>
              <li className={hasNumber ? "text-green-400" : "text-gray-500"}>
                {hasNumber ? "\u2713" : "\u00b7"} One number
              </li>
            </ul>
          </div>

          <div>
            <label className="text-[11px] text-gray-400 block mb-1">
              Confirm Password <span className="text-loss">*</span>
            </label>
            <input
              type="password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              placeholder="Repeat password"
              className="w-full h-10 px-3 bg-surface border border-gray-700/60 rounded-lg text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-lotus/60 transition"
              autoComplete="new-password"
            />
          </div>

          <div>
            <label className="text-[11px] text-gray-400 block mb-1">
              Referral Code <span className="text-gray-600">(optional)</span>
            </label>
            <input
              type="text"
              value={referralCode}
              onChange={(e) => setReferralCode(e.target.value.toUpperCase())}
              placeholder="Enter referral code"
              className="w-full h-10 px-3 bg-surface border border-gray-700/60 rounded-lg text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-lotus/60 transition font-mono"
            />
          </div>

          <label className="flex items-start gap-2 text-[11px] text-gray-400 pt-1">
            <input
              type="checkbox"
              checked={acceptedTerms}
              onChange={(e) => setAcceptedTerms(e.target.checked)}
              className="mt-0.5 accent-lotus"
              required
            />
            <span>
              I am 18 or older and I accept the{" "}
              <Link href="/terms" className="text-lotus hover:underline">
                Terms &amp; Conditions
              </Link>{" "}
              and{" "}
              <Link href="/privacy" className="text-lotus hover:underline">
                Privacy Policy
              </Link>
            </span>
          </label>

          {error && (
            <div className="text-xs text-loss bg-loss/10 border border-loss/20 rounded-lg px-3 py-2">
              {error}
            </div>
          )}

          <button
            type="submit"
            disabled={loading}
            className="w-full h-10 bg-lotus hover:bg-lotus-light text-white rounded-lg text-sm font-semibold transition disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {loading ? "Creating account..." : "Create Account"}
          </button>
        </form>

        <p className="text-center text-xs text-gray-500 mt-5">
          Already have an account?{" "}
          <Link href="/login" className="text-lotus hover:underline">
            Sign In
          </Link>
        </p>
        <p className="text-center text-[10px] text-gray-500 mt-3">
          18+ | Gamble Responsibly | 256-bit Encrypted
        </p>
      </div>
    </div>
  );
}
