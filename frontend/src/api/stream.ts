/*
 * The event stream.
 *
 * The gateway pushes what happens — a server connecting, tools
 * changing, a tool call finishing — rather than being polled for it.
 * This is the client half: it keeps a connection open, reconnects when
 * it drops, and hands each event to a listener.
 *
 * It deliberately knows nothing about React or about what the events
 * mean. The store decides what to do with them.
 */

export const EVENT_KINDS = [
  'server.connected',
  'server.disconnected',
  'server.status',
  'server.failed',
  'tools.changed',
  'resources.changed',
  'config.updated',
  'toolcall.started',
  'toolcall.completed',
  'toolcall.failed',
] as const;

export type EventKind = (typeof EVENT_KINDS)[number];

export interface GatewayEvent {
  kind: EventKind;
  at: string;
  server?: string;
  data?: unknown;
}

/** What the server sends. */
interface ServerMessage {
  type: 'event' | 'welcome' | 'error';
  event?: GatewayEvent;
  kinds?: EventKind[];
  message?: string;
}

export type Connection = 'connecting' | 'open' | 'closed';

export interface StreamHandlers {
  onEvent: (event: GatewayEvent) => void;
  onConnectionChange: (state: Connection) => void;
}

/** Backoff between reconnection attempts. The first retry is quick,
 *  because much the most common cause is the gateway restarting during
 *  development and being back within a second. */
const BACKOFF_MS = [500, 1000, 2000, 4000, 8000, 15_000];

/** How long a connection must stay up before it counts as healthy.
 *  Without this, a connection that is accepted and immediately dropped
 *  would reset the backoff every time and become a tight retry loop. */
const STABLE_AFTER_MS = 5_000;

export class EventStream {
  private socket: WebSocket | null = null;
  private attempt = 0;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private openedAt = 0;
  private stopped = false;

  private readonly handlers: StreamHandlers;
  private readonly url: string;

  constructor(handlers: StreamHandlers, url: string = defaultURL()) {
    this.handlers = handlers;
    this.url = url;
  }

  start(): void {
    this.stopped = false;
    this.open();
  }

  stop(): void {
    this.stopped = true;
    if (this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
    }
    // Detach the handlers before closing so the close does not schedule
    // a reconnection to something that has been deliberately stopped.
    const socket = this.socket;
    this.socket = null;
    if (socket) {
      socket.onopen = null;
      socket.onclose = null;
      socket.onerror = null;
      socket.onmessage = null;
      socket.close();
    }
    this.handlers.onConnectionChange('closed');
  }

  /** Narrows what this client receives. An empty list means everything. */
  subscribe(kinds: EventKind[]): void {
    this.send({ action: 'subscribe', kinds });
  }

  private send(message: unknown): void {
    if (this.socket?.readyState === WebSocket.OPEN) {
      this.socket.send(JSON.stringify(message));
    }
  }

  private open(): void {
    if (this.stopped) return;

    this.handlers.onConnectionChange('connecting');

    let socket: WebSocket;
    try {
      socket = new WebSocket(this.url);
    } catch {
      this.scheduleReconnect();
      return;
    }
    this.socket = socket;

    socket.onopen = () => {
      this.openedAt = Date.now();
      this.handlers.onConnectionChange('open');
    };

    socket.onmessage = (raw: MessageEvent<string>) => {
      let message: ServerMessage;
      try {
        message = JSON.parse(raw.data) as ServerMessage;
      } catch {
        return; // Not something this client can act on.
      }
      if (message.type === 'event' && message.event) {
        this.handlers.onEvent(message.event);
      }
    };

    socket.onerror = () => {
      // A websocket error is always followed by a close, which is where
      // reconnection is handled. Acting here too would open two.
    };

    socket.onclose = () => {
      if (this.socket !== socket) return; // Superseded or stopped.
      this.socket = null;
      if (this.stopped) return;

      // A connection that lasted counts as healthy, so the next outage
      // starts its backoff from the beginning rather than from wherever
      // the previous one left off.
      if (this.openedAt && Date.now() - this.openedAt > STABLE_AFTER_MS) {
        this.attempt = 0;
      }
      this.scheduleReconnect();
    };
  }

  private scheduleReconnect(): void {
    if (this.stopped) return;

    this.handlers.onConnectionChange('closed');

    const delay = BACKOFF_MS[Math.min(this.attempt, BACKOFF_MS.length - 1)] ?? 15_000;
    this.attempt += 1;
    this.timer = setTimeout(() => {
      this.timer = null;
      this.open();
    }, delay);
  }
}

/** The stream lives on the same origin as the page: in production the
 *  binary serves both, and in development the dev server proxies it. */
function defaultURL(): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${window.location.host}/ws`;
}
