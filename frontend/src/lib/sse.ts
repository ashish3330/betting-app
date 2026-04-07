// Server-Sent Events client for real-time odds updates.
// Uses EventSource for efficient server push — single HTTP connection,
// no WebSocket handshake, auto-reconnect built in.
//
// Falls back to polling if SSE is unavailable.

type OddsUpdate = {
  id: string;
  name: string;
  sport: string;
  status: string;
  in_play: boolean;
  total_matched: number;
  runners: unknown[];
  updated_at: string;
};

type SSEHandler = (markets: OddsUpdate[]) => void;

class OddsSSEClient {
  private source: EventSource | null = null;
  private handlers: Set<SSEHandler> = new Set();
  private sport: string = "";
  private reconnectAttempts = 0;
  private maxReconnect = 10;
  private fallbackInterval: ReturnType<typeof setInterval> | null = null;

  connect(sport: string = "cricket") {
    this.sport = sport;
    this.disconnect();

    const baseURL = process.env.NEXT_PUBLIC_API_URL || "";
    const url = `${baseURL}/api/v1/stream/odds?sport=${sport}`;

    try {
      this.source = new EventSource(url);
      this.reconnectAttempts = 0;

      this.source.onmessage = (event) => {
        try {
          const markets: OddsUpdate[] = JSON.parse(event.data);
          this.handlers.forEach((h) => h(markets));
        } catch {
          // Malformed data
        }
      };

      this.source.onerror = () => {
        this.source?.close();
        this.source = null;
        this.scheduleReconnect();
      };
    } catch {
      // SSE not supported — fall back to polling
      this.startFallbackPolling();
    }
  }

  private scheduleReconnect() {
    if (this.reconnectAttempts >= this.maxReconnect) {
      this.startFallbackPolling();
      return;
    }
    this.reconnectAttempts++;
    const delay = Math.min(1000 * Math.pow(1.5, this.reconnectAttempts), 30000);
    setTimeout(() => this.connect(this.sport), delay);
  }

  private startFallbackPolling() {
    if (this.fallbackInterval) return;
    const baseURL = process.env.NEXT_PUBLIC_API_URL || "";
    this.fallbackInterval = setInterval(async () => {
      try {
        const res = await fetch(`${baseURL}/api/v1/markets?sport=${this.sport}`);
        const data = await res.json();
        if (Array.isArray(data)) {
          this.handlers.forEach((h) => h(data));
        }
      } catch {
        // Silent fail
      }
    }, 5000);
  }

  subscribe(handler: SSEHandler): () => void {
    this.handlers.add(handler);
    return () => this.handlers.delete(handler);
  }

  switchSport(sport: string) {
    if (this.sport === sport) return;
    this.connect(sport);
  }

  disconnect() {
    this.source?.close();
    this.source = null;
    if (this.fallbackInterval) {
      clearInterval(this.fallbackInterval);
      this.fallbackInterval = null;
    }
  }

  isConnected(): boolean {
    return this.source?.readyState === EventSource.OPEN;
  }
}

// Singleton
let instance: OddsSSEClient | null = null;

export function getOddsSSE(): OddsSSEClient {
  if (!instance) {
    instance = new OddsSSEClient();
  }
  return instance;
}

export type { OddsUpdate, SSEHandler, OddsSSEClient };
