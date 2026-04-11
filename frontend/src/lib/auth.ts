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
import { api, ApiError, User, WalletBalance } from "./api";
import { decryptLocalStorage } from "./crypto";

interface LoginResult {
  user: User;
  requires_otp?: boolean;
  user_id?: number;
  otp_code?: string;
}

interface AuthState {
  user: User | null;
  isLoggedIn: boolean;
  isLoading: boolean;
  balance: WalletBalance | null;
  login: (username: string, password: string) => Promise<LoginResult>;
  demoLogin: () => Promise<{ user: User }>;
  logout: () => Promise<void>;
  refreshBalance: () => Promise<void>;
}

const AuthContext = createContext<AuthState>({
  user: null,
  isLoggedIn: false,
  isLoading: true,
  balance: null,
  login: async () => ({ user: {} as User }),
  demoLogin: async () => ({ user: {} as User }),
  logout: async () => {},
  refreshBalance: async () => {},
});

/**
 * Read the cached user from localStorage. Returns `null` on the server (no
 * window) or when no cached user exists. Used as a synchronous lazy initialiser
 * for the AuthProvider state so the first client render already reflects the
 * logged-in UI and we don't see a logged-out flicker.
 */
function readStoredUser(): User | null {
  if (typeof window === "undefined") return null;
  const stored = decryptLocalStorage("user");
  if (!stored) return null;
  try {
    return JSON.parse(stored) as User;
  } catch {
    try {
      localStorage.removeItem("user");
    } catch {
      // ignore
    }
    return null;
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  // Lazy initialiser: synchronously hydrate from localStorage on the first
  // client render. SSR has no window, so it returns `null` (logged out) — the
  // first client render will then immediately produce the logged-in UI without
  // a flicker. The hydration mismatch is suppressed in the Navbar.
  const [user, setUser] = useState<User | null>(() => readStoredUser());
  // We're effectively done loading after the synchronous hydration.
  const [isLoading, setIsLoading] = useState(false);
  const [balance, setBalance] = useState<WalletBalance | null>(null);

  useEffect(() => {
    // Defensive: if for any reason the lazy initialiser missed a stored user
    // (e.g. value was written between component construction and mount),
    // pick it up here. Also marks loading complete in either branch.
    if (!user) {
      const stored = readStoredUser();
      if (stored) setUser(stored);
    }
    setIsLoading(false);
    // We intentionally only run this once on mount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const refreshBalance = useCallback(async () => {
    // Authentication is now carried by an HttpOnly cookie that JS can't read,
    // so we can't gate on a token presence check. Just attempt the call and
    // let a 401 reset auth state.
    try {
      const bal = await api.getBalance();
      setBalance(bal);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setUser(null);
        setBalance(null);
        if (typeof window !== "undefined") {
          localStorage.removeItem("user");
        }
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

  const login = async (username: string, password: string): Promise<LoginResult> => {
    const data = await api.login(username, password);
    if (data.user) {
      setUser(data.user);
    }
    return {
      user: (data.user ?? ({} as User)) as User,
      requires_otp: data.requires_otp,
      user_id: data.user_id,
      otp_code: data.otp_code,
    };
  };

  const demoLogin = async () => {
    const data = await api.demoLogin();
    if (data.user) {
      setUser(data.user);
    }
    return data;
  };

  const logout = async (): Promise<void> => {
    // Fire-and-forget the backend logout call so the HttpOnly cookies are
    // cleared by Set-Cookie. Local user cache is wiped immediately.
    setUser(null);
    setBalance(null);
    try {
      await api.logout();
    } catch {
      // ignore — local state already cleared
    }
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
