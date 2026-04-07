"use client";

import { useState, useEffect } from "react";
import { useAuth } from "@/lib/auth";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { getTheme, type Theme } from "@/lib/theme";

export default function LoginPage() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [demoLoading, setDemoLoading] = useState(false);
  const [showPassword, setShowPassword] = useState(false);
  const [theme, setTheme] = useState<Theme>("dark");
  const { login, demoLogin } = useAuth();

  useEffect(() => { setTheme(getTheme()); }, []);
  const router = useRouter();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!username || !password) { setError("Please enter username and password"); return; }
    setLoading(true);
    setError("");
    try {
      const result = await login(username, password);
      const role = result?.user?.role || "client";
      router.push(role === "client" ? "/" : "/panel");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Invalid credentials");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-[calc(100vh-100px)] flex items-center justify-center px-4">
      <div className="w-full max-w-sm">
        {/* Logo */}
        <div className="text-center mb-6">
          <img src={theme === "dark" ? "/logo.svg?v=3" : "/logo-light.svg?v=3"} alt="3XBet" className="h-16 w-auto mx-auto mb-3" />
          <h1 className="text-lg font-bold text-white">Login</h1>
          <p className="text-xs text-gray-400 mt-1">3XBet</p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-3">
          <div>
            <label className="text-[11px] text-gray-400 block mb-1">Username</label>
            <input
              type="text" value={username} onChange={(e) => setUsername(e.target.value)}
              placeholder="Enter username" autoComplete="username"
              className="w-full h-10 px-3 bg-surface border border-gray-700/60 rounded-lg text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-lotus/60 transition"
            />
          </div>
          <div>
            <label className="text-[11px] text-gray-400 block mb-1">Password</label>
            <div className="relative">
              <input
                type={showPassword ? "text" : "password"} value={password} onChange={(e) => setPassword(e.target.value)}
                placeholder="Enter password" autoComplete="current-password"
                className="w-full h-10 px-3 pr-10 bg-surface border border-gray-700/60 rounded-lg text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-lotus/60 transition"
              />
              <button type="button" onClick={() => setShowPassword(!showPassword)}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 text-xs">
                {showPassword ? "Hide" : "Show"}
              </button>
            </div>
          </div>

          {error && (
            <div className="text-xs text-loss bg-loss/10 border border-loss/20 rounded-lg px-3 py-2">{error}</div>
          )}

          <button type="submit" disabled={loading}
            className="w-full h-10 bg-lotus hover:bg-lotus-light text-white rounded-lg text-sm font-semibold transition disabled:opacity-50">
            {loading ? "Signing in..." : "Sign In"}
          </button>
        </form>

        {/* Divider */}
        <div className="flex items-center gap-3 mt-4">
          <div className="flex-1 h-px bg-gray-700/40" />
          <span className="text-[10px] text-gray-500 uppercase tracking-wider">or</span>
          <div className="flex-1 h-px bg-gray-700/40" />
        </div>

        {/* Demo Button */}
        <button
          type="button"
          disabled={demoLoading}
          onClick={async () => {
            setDemoLoading(true);
            setError("");
            try {
              await demoLogin();
              router.push("/");
            } catch (err) {
              setError(err instanceof Error ? err.message : "Demo login failed");
            } finally {
              setDemoLoading(false);
            }
          }}
          className="w-full mt-3 h-10 bg-white/5 hover:bg-white/10 border border-gray-700/60 text-white rounded-lg text-sm font-semibold transition flex items-center justify-center gap-2 disabled:opacity-50"
        >
          {demoLoading ? (
            <span className="w-4 h-4 border-2 border-gray-500 border-t-white rounded-full animate-spin" />
          ) : (
            <svg className="w-4 h-4 text-lotus" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          )}
          {demoLoading ? "Creating Demo..." : "Try Demo — ₹1,00,000 Free"}
        </button>
        <p className="text-center text-[10px] text-gray-500 mt-1">
          Instant access, no registration needed
        </p>

        {/* Quick login for testing */}
        <div className="mt-4 bg-surface rounded-lg border border-gray-800/60 p-3">
          <p className="text-[10px] text-gray-500 mb-2 font-medium">Quick Login (Testing)</p>
          <div className="grid grid-cols-2 gap-1">
            {[
              { user: "player1", pass: "Player@123", label: "Player" },
              { user: "agent1", pass: "Agent@123", label: "Agent" },
              { user: "master1", pass: "Master@123", label: "Master" },
              { user: "superadmin", pass: "Admin@123", label: "Admin" },
            ].map((d) => (
              <button key={d.user} type="button"
                onClick={() => { setUsername(d.user); setPassword(d.pass); }}
                className="text-[11px] text-gray-400 hover:text-white bg-surface-light hover:bg-surface-lighter rounded px-2 py-1.5 text-left transition">
                <span className="text-gray-500">{d.label}:</span> <span className="font-mono">{d.user}</span>
              </button>
            ))}
          </div>
        </div>

        <p className="text-center text-xs text-gray-500 mt-4">
          Need an account? <Link href="/register" className="text-lotus hover:underline">Register</Link>
        </p>
        <p className="text-center text-[10px] text-gray-500 mt-3">18+ | Gamble Responsibly | 256-bit Encrypted</p>
      </div>
    </div>
  );
}
