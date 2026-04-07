"use client";

import {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
  ReactNode,
} from "react";
import React from "react";
import { api, User, WalletBalance } from "./api";
import { decryptLocalStorage } from "./crypto";

interface AuthState {
  user: User | null;
  isLoggedIn: boolean;
  isLoading: boolean;
  balance: WalletBalance | null;
  login: (username: string, password: string) => Promise<{ user: User; access_token: string; refresh_token: string }>;
  demoLogin: () => Promise<{ user: User }>;
  logout: () => void;
  refreshBalance: () => Promise<void>;
}

const AuthContext = createContext<AuthState>({
  user: null,
  isLoggedIn: false,
  isLoading: true,
  balance: null,
  login: async () => ({ user: {} as User, access_token: "", refresh_token: "" }),
  demoLogin: async () => ({ user: {} as User }),
  logout: () => {},
  refreshBalance: async () => {},
});

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [balance, setBalance] = useState<WalletBalance | null>(null);

  useEffect(() => {
    // Check for stored user on mount
    const stored = decryptLocalStorage("user");
    const token = decryptLocalStorage("access_token");
    if (stored && token) {
      try {
        setUser(JSON.parse(stored));
      } catch {
        localStorage.removeItem("user");
      }
    }
    setIsLoading(false);
  }, []);

  const refreshBalance = useCallback(async () => {
    if (!decryptLocalStorage("access_token")) return;
    try {
      const bal = await api.getBalance();
      setBalance(bal);
    } catch (err) {
      // If session expired (tokens cleared by api.ts), reset auth state
      if (!decryptLocalStorage("access_token")) {
        setUser(null);
        setBalance(null);
      }
    }
  }, []);

  useEffect(() => {
    if (user) {
      refreshBalance();
      const interval = setInterval(refreshBalance, 15000);
      return () => clearInterval(interval);
    }
  }, [user, refreshBalance]);

  const login = async (username: string, password: string) => {
    const data = await api.login(username, password);
    if (data.user) {
      setUser(data.user);
    }
    return data as { user: User; access_token: string; refresh_token: string; requires_otp?: boolean; user_id?: number; otp_code?: string };
  };

  const demoLogin = async () => {
    const data = await api.demoLogin();
    if (data.user) {
      setUser(data.user);
    }
    return data;
  };

  const logout = () => {
    localStorage.removeItem("access_token");
    localStorage.removeItem("refresh_token");
    localStorage.removeItem("user");
    setUser(null);
    setBalance(null);
  };

  return React.createElement(
    AuthContext.Provider,
    {
      value: {
        user,
        isLoggedIn: !!user,
        isLoading,
        balance,
        login,
        demoLogin,
        logout,
        refreshBalance,
      },
    },
    children
  );
}

export function useAuth() {
  return useContext(AuthContext);
}

export function getStoredUser(): User | null {
  if (typeof window === "undefined") return null;
  const stored = decryptLocalStorage("user");
  if (!stored) return null;
  try {
    return JSON.parse(stored);
  } catch {
    return null;
  }
}

export function isAdmin(): boolean {
  const user = getStoredUser();
  return user?.role === "admin" || user?.role === "superadmin";
}

export function isSuperAdmin(): boolean {
  const user = getStoredUser();
  return user?.role === "superadmin";
}

export function isAgent(): boolean {
  const user = getStoredUser();
  return user?.role === "agent" || user?.role === "master" || user?.role === "admin" || user?.role === "superadmin";
}

export function isClient(): boolean {
  const user = getStoredUser();
  return user?.role === "client";
}

export type UserRole = "superadmin" | "admin" | "master" | "agent" | "client";

export function getUserRole(): UserRole | null {
  const user = getStoredUser();
  return (user?.role as UserRole) || null;
}
