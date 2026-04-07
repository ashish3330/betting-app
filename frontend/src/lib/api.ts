import { decryptData, encryptData, encryptLocalStorage, decryptLocalStorage } from "./crypto";

const BASE_URL = process.env.NEXT_PUBLIC_API_URL || "";

interface ApiOptions extends RequestInit {
  auth?: boolean;
  /** Cache GET results for N milliseconds. Default 0 (no cache). */
  cacheTTL?: number;
}

// ── Request deduplication + short-lived cache ──────────────────────────────
const inflightRequests = new Map<string, Promise<unknown>>();
const responseCache = new Map<string, { data: unknown; expiry: number }>();

function getCacheKey(endpoint: string, method?: string): string {
  return `${method || "GET"}:${endpoint}`;
}

class ApiClient {
  private refreshPromise: Promise<boolean> | null = null;

  private getToken(): string | null {
    if (typeof window === "undefined") return null;
    return decryptLocalStorage("access_token");
  }

  private getRefreshToken(): string | null {
    if (typeof window === "undefined") return null;
    return decryptLocalStorage("refresh_token");
  }

  async request<T>(endpoint: string, options: ApiOptions = {}): Promise<T> {
    const { auth = false, headers: customHeaders, cacheTTL = 0, ...rest } = options;
    const method = (rest.method || "GET").toUpperCase();
    const cacheKey = getCacheKey(endpoint, method);

    // 1. Return cached response if valid (GET only)
    if (method === "GET" && cacheTTL > 0) {
      const cached = responseCache.get(cacheKey);
      if (cached && Date.now() < cached.expiry) {
        return cached.data as T;
      }
    }

    // 2. Deduplicate inflight GET requests
    if (method === "GET") {
      const inflight = inflightRequests.get(cacheKey);
      if (inflight) return inflight as Promise<T>;
    }

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      ...(customHeaders as Record<string, string>),
    };

    if (auth) {
      const token = this.getToken();
      if (token) {
        headers["Authorization"] = `Bearer ${token}`;
      }
    }

    // Encrypt POST/PUT request bodies
    if ((method === "POST" || method === "PUT") && rest.body && typeof rest.body === "string") {
      const originalBody = JSON.parse(rest.body as string);
      const encrypted = await encryptData(originalBody);
      rest.body = JSON.stringify({ d: encrypted });
    }

    const promise = fetch(`${BASE_URL}${endpoint}`, { headers, ...rest });

    // Track inflight GETs
    if (method === "GET") {
      const wrappedPromise = promise.then(async (response) => {
        inflightRequests.delete(cacheKey);
        return this.handleResponse<T>(response, endpoint, options, headers, cacheKey, cacheTTL);
      }).catch((err) => {
        inflightRequests.delete(cacheKey);
        throw err;
      });
      inflightRequests.set(cacheKey, wrappedPromise);
      return wrappedPromise;
    }

    const response = await promise;
    return this.handleResponse<T>(response, endpoint, options, headers, cacheKey, cacheTTL);
  }

  private async handleResponse<T>(
    response: Response,
    endpoint: string,
    options: ApiOptions,
    headers: Record<string, string>,
    cacheKey: string,
    cacheTTL: number,
  ): Promise<T> {
    const { auth = false, ...rest } = options;

    if (response.status === 401 && auth) {
      const refreshed = await this.refreshToken();
      if (refreshed) {
        headers["Authorization"] = `Bearer ${this.getToken()}`;
        const retryResponse = await fetch(`${BASE_URL}${endpoint}`, {
          headers,
          ...rest,
        });
        if (!retryResponse.ok) {
          throw new ApiError(retryResponse.status, await retryResponse.text());
        }
        let retryData = await retryResponse.json();
        if (retryData && typeof retryData === "object" && typeof retryData.d === "string" && Object.keys(retryData).length === 1) {
          try {
            retryData = await decryptData(retryData.d);
          } catch {
            // fallback to raw
          }
        }
        return retryData;
      } else {
        // Clear tokens but don't redirect — let components decide
        if (typeof window !== "undefined") {
          localStorage.removeItem("access_token");
          localStorage.removeItem("refresh_token");
          localStorage.removeItem("user");
        }
        throw new ApiError(401, "Session expired. Please login again.");
      }
    }

    if (!response.ok) {
      // Error responses are also encrypted — try to decrypt them
      let errorMsg = `Request failed with status ${response.status}`;
      try {
        const errJson = await response.json();
        if (errJson && typeof errJson.d === "string" && Object.keys(errJson).length === 1) {
          const decrypted = await decryptData<{ error?: string }>(errJson.d);
          errorMsg = decrypted?.error || JSON.stringify(decrypted);
        } else {
          errorMsg = errJson?.error || JSON.stringify(errJson);
        }
      } catch {
        try {
          errorMsg = await response.text();
        } catch {
          // give up
        }
      }
      throw new ApiError(response.status, errorMsg);
    }

    let data = await response.json();

    // Decrypt if response is encrypted: { d: "<base64>" }
    if (data && typeof data === "object" && typeof data.d === "string" && Object.keys(data).length === 1) {
      try {
        data = await decryptData(data.d);
      } catch {
        // If decryption fails, use raw data (backwards compatible)
      }
    }

    // Cache successful GET responses
    if (cacheTTL > 0 && (rest.method || "GET").toUpperCase() === "GET") {
      responseCache.set(cacheKey, { data, expiry: Date.now() + cacheTTL });
    }

    return data as T;
  }

  private async refreshToken(): Promise<boolean> {
    if (this.refreshPromise) return this.refreshPromise;
    this.refreshPromise = this._doRefresh();
    try {
      return await this.refreshPromise;
    } finally {
      this.refreshPromise = null;
    }
  }

  private async _doRefresh(): Promise<boolean> {
    const refreshToken = this.getRefreshToken();
    if (!refreshToken) return false;
    try {
      const res = await fetch(`${BASE_URL}/api/v1/auth/refresh`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refresh_token: refreshToken }),
      });
      if (!res.ok) return false;
      let data = await res.json();

      // Decrypt if response is encrypted
      if (data && typeof data === "object" && typeof data.d === "string" && Object.keys(data).length === 1) {
        try {
          data = await decryptData(data.d);
        } catch {
          // decryption failed
        }
      }

      encryptLocalStorage("access_token", data.access_token);
      if (data.refresh_token) {
        encryptLocalStorage("refresh_token", data.refresh_token);
      }
      return true;
    } catch {
      return false;
    }
  }

  private logout() {
    if (typeof window === "undefined") return;
    localStorage.removeItem("access_token");
    localStorage.removeItem("refresh_token");
    localStorage.removeItem("user");
    window.location.href = "/login";
  }

  // Auth
  async login(username: string, password: string) {
    const data = await this.request<{
      access_token?: string;
      refresh_token?: string;
      user?: User;
      csrf_token?: string;
      requires_otp?: boolean;
      user_id?: number;
      otp_code?: string;
    }>("/api/v1/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    });

    // If OTP is required, return the data without storing tokens
    if (data.requires_otp) {
      return data;
    }

    if (data.access_token) {
      encryptLocalStorage("access_token", data.access_token);
    }
    if (data.refresh_token) {
      encryptLocalStorage("refresh_token", data.refresh_token);
    }
    if (data.user) {
      encryptLocalStorage("user", JSON.stringify(data.user));
    }
    if (data.csrf_token) {
      encryptLocalStorage("csrf_token", data.csrf_token);
    }
    return data;
  }

  async completeOTPLogin(userId: number, code: string) {
    const data = await this.verifyOTP(userId, code);
    encryptLocalStorage("access_token", data.access_token);
    encryptLocalStorage("refresh_token", data.refresh_token);
    encryptLocalStorage("user", JSON.stringify(data.user));
    if (data.csrf_token) {
      encryptLocalStorage("csrf_token", data.csrf_token);
    }
    return data;
  }

  async demoLogin() {
    const data = await this.request<{
      access_token: string;
      refresh_token: string;
      user: User;
      csrf_token?: string;
      is_demo: boolean;
    }>("/api/v1/auth/demo", { method: "POST" });

    if (data.access_token) encryptLocalStorage("access_token", data.access_token);
    if (data.refresh_token) encryptLocalStorage("refresh_token", data.refresh_token);
    if (data.user) encryptLocalStorage("user", JSON.stringify(data.user));
    if (data.csrf_token) encryptLocalStorage("csrf_token", data.csrf_token);
    return data;
  }

  async register(username: string, email: string, password: string, role = "user") {
    return this.request("/api/v1/auth/register", {
      method: "POST",
      body: JSON.stringify({ username, email, password, role }),
    });
  }

  async changePassword(oldPassword: string, newPassword: string) {
    return this.request<{ message: string }>("/api/v1/auth/change-password", {
      method: "POST",
      auth: true,
      body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
    });
  }

  // Sports
  async fetchSports() {
    return this.request<Sport[]>("/api/v1/sports", { cacheTTL: 60000 }); // 1min cache
  }

  async fetchCompetitions(sport: string) {
    return this.request<Competition[]>(`/api/v1/competitions?sport=${sport}`, { cacheTTL: 30000 }); // 30s
  }

  async fetchEvents(competitionId: string) {
    return this.request<SportEvent[]>(`/api/v1/events?competition_id=${competitionId}`, { cacheTTL: 10000 }); // 10s
  }

  async fetchEventsBySport(sport: string) {
    return this.request<SportEvent[]>(`/api/v1/events?sport=${sport}`, { cacheTTL: 10000 });
  }

  async fetchEventMarkets(eventId: string) {
    return this.request<Market[]>(`/api/v1/events/${eventId}/markets`, { cacheTTL: 10000 });
  }

  // Markets
  async getMarkets(sport = "cricket") {
    return this.request<Market[]>(`/api/v1/markets?sport=${sport}`, { cacheTTL: 3000 });
  }

  async getMarketOdds(marketId: string) {
    return this.request<MarketOdds>(`/api/v1/markets/${marketId}/odds`, { cacheTTL: 2000 });
  }

  async getLiveScore(eventId: string) {
    return this.request<LiveScore>(`/api/v1/scores/${eventId}`, { cacheTTL: 5000 });
  }

  async getOrderBook(marketId: string) {
    return this.request<OrderBook>(`/api/v1/market/${marketId}/orderbook`);
  }

  /** Fetch user P&L positions per runner (selectionId -> P&L if that runner wins) */
  async getPositions(marketId: string): Promise<Record<string, number>> {
    return this.request<Record<string, number>>(`/api/v1/positions/${marketId}`, { auth: true });
  }

  // Bets
  async placeBet(bet: PlaceBetRequest) {
    return this.request<PlaceBetResponse>("/api/v1/bet/place", {
      method: "POST",
      auth: true,
      body: JSON.stringify(bet),
    });
  }

  async fetchBettingHistory(filters: BettingHistoryFilters) {
    const params = new URLSearchParams();
    if (filters.sport) params.set("sport", filters.sport);
    if (filters.status) params.set("status", filters.status);
    if (filters.from) params.set("from", filters.from);
    if (filters.to) params.set("to", filters.to);
    if (filters.market) params.set("market", filters.market);
    const qs = params.toString();
    return this.request<BettingHistoryResponse>(`/api/v1/bets/history${qs ? `?${qs}` : ""}`, { auth: true });
  }

  // Wallet
  async getBalance() {
    return this.request<WalletBalance>("/api/v1/wallet/balance", { auth: true });
  }

  async getLedger() {
    return this.request<LedgerEntry[]>("/api/v1/wallet/ledger", { auth: true });
  }

  async fetchAccountStatement(from: string, to: string) {
    return this.request<LedgerEntry[]>(`/api/v1/wallet/statement?from=${from}&to=${to}`, { auth: true });
  }

  async initiateUPIDeposit(amount: number, upiId: string) {
    return this.request<DepositResponse>("/api/v1/wallet/deposit/upi", {
      method: "POST",
      auth: true,
      body: JSON.stringify({ amount, upi_id: upiId }),
    });
  }

  async initiateCryptoDeposit(amount: number, currency: string) {
    return this.request<CryptoDepositResponse>("/api/v1/wallet/deposit/crypto", {
      method: "POST",
      auth: true,
      body: JSON.stringify({ amount, currency }),
    });
  }

  async initiateWithdrawal(amount: number, method: string, details: WithdrawalDetails) {
    return this.request<WithdrawalResponse>("/api/v1/wallet/withdraw", {
      method: "POST",
      auth: true,
      body: JSON.stringify({ amount, method, details }),
    });
  }

  async getDepositHistory() {
    return this.request<TransactionRecord[]>("/api/v1/wallet/deposits", { auth: true });
  }

  async getWithdrawalHistory() {
    return this.request<TransactionRecord[]>("/api/v1/wallet/withdrawals", { auth: true });
  }

  // Casino
  async getCasinoProviders() {
    return this.request<CasinoProvider[]>("/api/v1/casino/providers");
  }

  async getCasinoGames() {
    return this.request<CasinoGame[]>("/api/v1/casino/games");
  }

  async fetchCasinoCategories() {
    return this.request<CasinoCategory[]>("/api/v1/casino/categories");
  }

  async fetchGamesByCategory(category: string) {
    return this.request<CasinoGame[]>(`/api/v1/casino/games?category=${category}`);
  }

  async createCasinoSession(gameType: string, providerId: string) {
    return this.request<CasinoSession>("/api/v1/casino/session", {
      method: "POST",
      auth: true,
      body: JSON.stringify({ game_type: gameType, provider_id: providerId }),
    });
  }

  // Cashout
  async getCashoutOffer(betId: string) {
    return this.request<{ offer: number }>(`/api/v1/cashout/offer/${betId}`, { auth: true });
  }

  async acceptCashout(betId: string) {
    return this.request<{ message: string; amount: number }>(`/api/v1/cashout/accept/${betId}`, {
      method: "POST",
      auth: true,
    });
  }

  // Notifications
  async fetchNotifications() {
    return this.request<Notification[]>("/api/v1/notifications", { auth: true });
  }

  async markNotificationRead(notificationId: string) {
    return this.request<{ message: string }>(`/api/v1/notifications/${notificationId}/read`, {
      method: "POST",
      auth: true,
    });
  }

  async markAllNotificationsRead() {
    return this.request<{ message: string }>("/api/v1/notifications/read-all", {
      method: "POST",
      auth: true,
    });
  }

  // Admin
  async getDashboard() {
    return this.request<AdminDashboard>("/api/v1/reports/dashboard", { auth: true });
  }

  // Responsible Gambling
  async getResponsibleLimits() {
    return this.request<ResponsibleGamblingLimits>("/api/v1/responsible/limits", { auth: true });
  }

  async setResponsibleLimits(limits: Partial<ResponsibleGamblingLimits>) {
    return this.request<ResponsibleGamblingLimits>("/api/v1/responsible/limits", {
      method: "POST",
      auth: true,
      body: JSON.stringify(limits),
    });
  }

  async selfExclude() {
    return this.request<{ message: string; excluded_until: string }>("/api/v1/responsible/self-exclude", {
      method: "POST",
      auth: true,
    });
  }

  // OTP / 2FA
  async generateOTP() {
    return this.request<{ message: string; otp_code: string }>("/api/v1/auth/otp/generate", {
      method: "POST",
      auth: true,
    });
  }

  async verifyOTP(userId: number, code: string) {
    return this.request<{
      access_token: string;
      refresh_token: string;
      user: User;
      csrf_token: string;
    }>("/api/v1/auth/otp/verify", {
      method: "POST",
      body: JSON.stringify({ user_id: userId, code }),
    });
  }

  async enableOTP(enable: boolean) {
    return this.request<{ message: string }>("/api/v1/auth/otp/enable", {
      method: "POST",
      auth: true,
      body: JSON.stringify({ enable }),
    });
  }

  // Sessions
  async getSessions() {
    return this.request<SessionInfo[]>("/api/v1/auth/sessions", { auth: true });
  }

  async logoutAllSessions() {
    return this.request<{ message: string }>("/api/v1/auth/sessions", {
      method: "DELETE",
      auth: true,
    });
  }

  async getLoginHistory(limit = 20) {
    return this.request<LoginHistoryRecord[]>(`/api/v1/auth/login-history?limit=${limit}`, { auth: true });
  }

  // Audit
  async getAuditLog(action?: string, username?: string) {
    const params = new URLSearchParams();
    if (action) params.set("action", action);
    if (username) params.set("username", username);
    const qs = params.toString();
    return this.request<AuditLogEntry[]>(`/api/v1/panel/audit${qs ? `?${qs}` : ""}`, { auth: true });
  }
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
    this.name = "ApiError";
  }
}

// Types
export interface User {
  id: number | string;
  username: string;
  email: string;
  role: string;
  balance?: number;
  exposure?: number;
  credit_limit?: number;
  commission_rate?: number;
  status: string;
  parent_id?: number;
  path?: string;
  created_at?: string;
  force_password_change?: boolean;
}

export interface Sport {
  id: string;
  name: string;
  slug: string;
  active_events: number;
  icon?: string;
}

export interface Competition {
  id: string;
  name: string;
  sport_id: string;
  sport?: string;
  region?: string;
  match_count: number;
  events_count?: number; // alias
  status?: string;
}

export interface SportEvent {
  id: string;
  name: string;
  competition_id: string;
  competition?: string;
  sport_id: string;
  sport?: string;
  start_time: string;
  in_play: boolean;
  status: string;
  market_id?: string;
  home_team?: string;
  away_team?: string;
  score?: string;
}

export interface Market {
  id: string;
  name: string;
  event_name: string;
  event_id?: string;
  sport: string;
  market_type?: string;
  status: string;
  in_play: boolean;
  start_time: string;
  total_matched?: number;
  runners: Runner[];
  competition?: string;
  venue?: string;
}

export interface PriceLevel {
  price: number;
  size: number;
}

export interface Runner {
  id: string;
  selection_id: number;
  name: string;
  status?: string;
  // New format from API (arrays of price levels)
  back_prices?: PriceLevel[];
  lay_prices?: PriceLevel[];
  // Legacy single-price fields (for backwards compat)
  back_price?: number;
  back_size?: number;
  lay_price?: number;
  lay_size?: number;
  // Fancy market fields
  run_value?: number;
  yes_rate?: number;
  no_rate?: number;
}

export interface MarketOdds {
  market_id: string;
  status: string;
  in_play: boolean;
  runners: Runner[];
  score?: LiveScore;
  event_name?: string;
  start_time?: string;
  timestamp?: string;
}

export interface LiveScore {
  home: string;
  away: string;
  home_score: string;
  away_score: string;
  overs?: string;
  run_rate?: string;
  required_rate?: string;
  last_wicket?: string;
  partnership?: string;
}

export interface OrderBookLevel {
  price: number;
  size: number;
}

export interface OrderBook {
  back: OrderBookLevel[];
  lay: OrderBookLevel[];
}

export interface PlaceBetRequest {
  market_id: string;
  selection_id: number;
  side: "back" | "lay";
  price: number;
  stake: number;
  client_ref: string;
}

export interface PlaceBetResponse {
  bet_id: string;
  status: string;
  matched_stake?: number;
  unmatched_stake?: number;
}

export interface WalletBalance {
  balance: number;
  exposure: number;
  available_balance: number;
}

export interface LedgerEntry {
  id: string;
  type: string;
  amount: number;
  balance_after: number;
  description: string;
  created_at: string;
}

export interface CasinoProvider {
  id: string;
  name: string;
  logo?: string;
}

export interface CasinoGame {
  id: string;
  name: string;
  type: string;
  provider_id: string;
  provider_name: string;
  image?: string;
  is_live: boolean;
  category?: string;
}

export interface CasinoCategory {
  id: string;
  name: string;
  slug: string;
  games_count: number;
}

export interface CasinoSession {
  session_id: string;
  url: string;
}

export interface DepositResponse {
  transaction_id: string;
  status: string;
  upi_link?: string;
  qr_code?: string;
}

export interface CryptoDepositResponse {
  transaction_id: string;
  status: string;
  wallet_address: string;
  currency: string;
  amount_crypto: number;
}

export interface WithdrawalDetails {
  upi_id?: string;
  bank_account?: string;
  bank_ifsc?: string;
  bank_name?: string;
  crypto_address?: string;
  crypto_currency?: string;
}

export interface WithdrawalResponse {
  transaction_id: string;
  status: string;
  estimated_time?: string;
}

export interface TransactionRecord {
  id: string;
  type: string;
  method: string;
  amount: number;
  status: string;
  created_at: string;
  completed_at?: string;
  details?: string;
}

export interface BettingHistoryFilters {
  sport?: string;
  status?: string;
  from?: string;
  to?: string;
  market?: string;
}

export interface BetRecord {
  id: string;
  market_id: string;
  market_name: string;
  event_name: string;
  sport: string;
  selection: string;
  side: "back" | "lay";
  odds: number;
  stake: number;
  status: string;
  pnl?: number;
  placed_at: string;
  settled_at?: string;
}

export interface BettingHistoryResponse {
  bets: BetRecord[];
  summary: {
    total_bets: number;
    total_stake: number;
    total_pnl: number;
    won: number;
    lost: number;
    pending: number;
  };
}

export interface Notification {
  id: string;
  type: string;
  title: string;
  message: string;
  read: boolean;
  created_at: string;
}

export interface AdminDashboard {
  active_users: number;
  bets_today: number;
  volume_today: number;
  revenue_today: number;
  markets_live: number;
  total_exposure: number;
}

export interface ResponsibleGamblingLimits {
  daily_deposit_limit: number;
  daily_loss_limit: number;
  session_limit_minutes: number;
  self_excluded: boolean;
  excluded_until?: string;
}

export interface SessionInfo {
  id: string;
  ip: string;
  user_agent: string;
  created_at: string;
  current: boolean;
}

export interface LoginHistoryRecord {
  user_id: number;
  ip: string;
  user_agent: string;
  login_at: string;
  success: boolean;
}

export interface AuditLogEntry {
  id: number;
  user_id: number;
  username: string;
  action: string;
  details: string;
  ip: string;
  timestamp: string;
}

export const api = new ApiClient();
