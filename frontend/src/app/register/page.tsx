"use client";

import { useState } from "react";
import { api } from "@/lib/api";
import { useRouter } from "next/navigation";
import Link from "next/link";

export default function RegisterPage() {
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const router = useRouter();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!username || !email || !password) {
      setError("All fields are required");
      return;
    }
    if (password !== confirmPassword) {
      setError("Passwords do not match");
      return;
    }
    if (password.length < 6) {
      setError("Password must be at least 6 characters");
      return;
    }

    setLoading(true);
    setError("");

    try {
      await api.register(username, email, password, "user");
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
    <div className="min-h-[80vh] flex items-center justify-center px-4">
      <div className="w-full max-w-sm">
        <div className="text-center mb-8">
          <div className="w-16 h-16 bg-lotus rounded-2xl flex items-center justify-center font-bold text-white text-2xl mx-auto mb-3">
            LE
          </div>
          <h1 className="text-xl font-bold text-white">Create Account</h1>
          <p className="text-sm text-gray-500 mt-1">
            Join 3XBet today
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="text-xs text-gray-400 block mb-1">
              Username
            </label>
            <input
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="Choose a username"
              className="w-full h-11 px-4 bg-surface border border-gray-700 rounded-xl text-sm text-white placeholder:text-gray-400 focus:outline-none focus:border-lotus transition"
              autoComplete="username"
            />
          </div>

          <div>
            <label className="text-xs text-gray-400 block mb-1">Email</label>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@example.com"
              className="w-full h-11 px-4 bg-surface border border-gray-700 rounded-xl text-sm text-white placeholder:text-gray-400 focus:outline-none focus:border-lotus transition"
              autoComplete="email"
            />
          </div>

          <div>
            <label className="text-xs text-gray-400 block mb-1">
              Password
            </label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="Min 6 characters"
              className="w-full h-11 px-4 bg-surface border border-gray-700 rounded-xl text-sm text-white placeholder:text-gray-400 focus:outline-none focus:border-lotus transition"
              autoComplete="new-password"
            />
          </div>

          <div>
            <label className="text-xs text-gray-400 block mb-1">
              Confirm Password
            </label>
            <input
              type="password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              placeholder="Repeat password"
              className="w-full h-11 px-4 bg-surface border border-gray-700 rounded-xl text-sm text-white placeholder:text-gray-400 focus:outline-none focus:border-lotus transition"
              autoComplete="new-password"
            />
          </div>

          {error && (
            <div className="text-xs text-loss bg-loss/10 border border-loss/20 rounded-lg px-3 py-2">
              {error}
            </div>
          )}

          <button
            type="submit"
            disabled={loading}
            className="w-full h-11 bg-lotus hover:bg-lotus-light text-white rounded-xl text-sm font-bold transition disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {loading ? "Creating account..." : "Create Account"}
          </button>
        </form>

        <p className="text-center text-xs text-gray-500 mt-6">
          Already have an account?{" "}
          <Link href="/login" className="text-lotus hover:underline">
            Sign In
          </Link>
        </p>
      </div>
    </div>
  );
}
