import { decryptLocalStorage } from "./crypto";

type MessageHandler = (data: unknown) => void;

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

    // Don't connect if no token (not logged in) or no subscriptions
    const token = typeof window !== "undefined" ? decryptLocalStorage("access_token") : null;
    if (!token && this.subscribedMarkets.length === 0) {
      return;
    }

    this.isConnecting = true;

    try {
      // Auth via query parameter — required by both the gateway proxy
      // (cmd/gateway/main.go:proxyWebSocket) and the odds-service WS
      // handler. Browser WebSocket cannot set Authorization headers,
      // and an in-band auth message is rejected because the server
      // already validated the token before the upgrade. Without this
      // every WS connection 401s and we silently fall back to 5s
      // polling on the markets page.
      const wsURL = token
        ? `${this.url}?token=${encodeURIComponent(token)}`
        : this.url;
      this.ws = new WebSocket(wsURL);

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
