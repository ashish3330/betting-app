type MessageHandler = (data: unknown) => void;

/**
 * Returns true if a user profile is cached in localStorage. Used as a
 * "logged-in" gate for the WebSocket connection now that the access_token
 * lives in an HttpOnly cookie that JavaScript cannot read.
 *
 * NOTE: WebSocket *authentication* against the gateway still requires the
 * server to read the access_token from cookies (or from a session derived
 * from them). That is a backend concern; this client just refrains from
 * leaking tokens via localStorage.
 */
function hasCachedUser(): boolean {
  if (typeof window === "undefined") return false;
  return localStorage.getItem("user") !== null;
}

interface WSMessage {
  type: string;
  payload: unknown;
}

class LotusWebSocket {
  private ws: WebSocket | null = null;
  private url: string;
  private handlers: Map<string, Set<MessageHandler>> = new Map();
  private reconnectAttempts = 0;
  private maxReconnect = 10;
  private reconnectDelay = 1000;
  private subscribedMarkets: string[] = [];
  private isConnecting = false;

  constructor() {
    const wsHost =
      typeof window !== "undefined"
        ? window.location.hostname
        : "localhost";
    // Base URL — the token query parameter is appended at connect() time
    // so each reconnect uses the freshest token from local storage.
    this.url = `ws://${wsHost}:8080/ws`;
  }

  connect() {
    if (this.isConnecting || (this.ws && this.ws.readyState === WebSocket.OPEN)) {
      return;
    }

    // Don't connect when there's nothing to do: no logged-in user AND
    // no public market subscriptions queued.
    if (!hasCachedUser() && this.subscribedMarkets.length === 0) {
      return;
    }

    this.isConnecting = true;

    try {
      // Auth: the access_token now lives in an HttpOnly cookie that
      // browser JS cannot read, but the browser DOES send same-origin
      // cookies on the WebSocket upgrade request automatically. The
      // gateway's proxyWebSocket reads access_token from the cookie
      // when no ?token= query param is present (see commit dd2134b).
      // No client-side auth frame is needed.
      this.ws = new WebSocket(this.url);

      this.ws.onopen = () => {
        this.isConnecting = false;
        this.reconnectAttempts = 0;

        // Re-subscribe to markets
        if (this.subscribedMarkets.length > 0) {
          this.send({
            type: "subscribe",
            payload: { market_ids: this.subscribedMarkets },
          });
        }
      };

      this.ws.onmessage = (event) => {
        try {
          const msg: WSMessage = JSON.parse(event.data);
          const handlers = this.handlers.get(msg.type);
          if (handlers) {
            handlers.forEach((handler) => handler(msg.payload));
          }
          // Also notify wildcard handlers
          const wildcardHandlers = this.handlers.get("*");
          if (wildcardHandlers) {
            wildcardHandlers.forEach((handler) => handler(msg));
          }
        } catch {
          // Ignore malformed messages
        }
      };

      this.ws.onclose = () => {
        this.isConnecting = false;
        this.ws = null;
        this.scheduleReconnect();
      };

      this.ws.onerror = () => {
        this.isConnecting = false;
      };
    } catch {
      this.isConnecting = false;
      this.scheduleReconnect();
    }
  }

  private scheduleReconnect() {
    if (this.reconnectAttempts >= this.maxReconnect) return;
    this.reconnectAttempts++;
    const delay = this.reconnectDelay * Math.pow(1.5, this.reconnectAttempts - 1);
    setTimeout(() => this.connect(), Math.min(delay, 30000));
  }

  private send(msg: WSMessage) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  subscribe(marketIds: string[]) {
    this.subscribedMarkets = [
      ...new Set([...this.subscribedMarkets, ...marketIds]),
    ];
    this.send({
      type: "subscribe",
      payload: { market_ids: marketIds },
    });
  }

  unsubscribe(marketIds: string[]) {
    this.subscribedMarkets = this.subscribedMarkets.filter(
      (id) => !marketIds.includes(id)
    );
    this.send({
      type: "unsubscribe",
      payload: { market_ids: marketIds },
    });
  }

  on(type: string, handler: MessageHandler): () => void {
    if (!this.handlers.has(type)) {
      this.handlers.set(type, new Set());
    }
    this.handlers.get(type)!.add(handler);

    // Return unsubscribe function
    return () => {
      this.handlers.get(type)?.delete(handler);
    };
  }

  disconnect() {
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    this.subscribedMarkets = [];
    this.reconnectAttempts = this.maxReconnect; // prevent reconnect
  }
}

// Singleton
let instance: LotusWebSocket | null = null;

export function getWS(): LotusWebSocket {
  if (!instance) {
    instance = new LotusWebSocket();
  }
  return instance;
}

export type { LotusWebSocket, MessageHandler };
