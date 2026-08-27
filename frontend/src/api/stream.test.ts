import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { EventStream, type Connection, type GatewayEvent } from './stream';

/**
 * A stand-in for the browser's WebSocket, controllable from the test.
 *
 * There is no way to make a real connection drop on demand in jsdom,
 * and reconnection is the whole behaviour under test — so the socket
 * itself is what gets replaced, and everything above it is the real
 * code.
 */
class FakeSocket {
  static instances: FakeSocket[] = [];
  static readonly OPEN = 1;

  readyState = 0;
  sent: string[] = [];

  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;

  readonly url: string;

  constructor(url: string) {
    this.url = url;
    FakeSocket.instances.push(this);
  }

  send(data: string): void {
    this.sent.push(data);
  }

  close(): void {
    this.readyState = 3;
    this.onclose?.();
  }

  // ===== test controls =====

  accept(): void {
    this.readyState = 1;
    this.onopen?.();
  }

  drop(): void {
    this.readyState = 3;
    this.onclose?.();
  }

  deliver(payload: unknown): void {
    this.onmessage?.({ data: JSON.stringify(payload) });
  }

  static get latest(): FakeSocket {
    const socket = FakeSocket.instances.at(-1);
    if (!socket) throw new Error('no socket was opened');
    return socket;
  }
}

const original = globalThis.WebSocket;

beforeEach(() => {
  FakeSocket.instances = [];
  vi.useFakeTimers();
  globalThis.WebSocket = FakeSocket as unknown as typeof WebSocket;
});

afterEach(() => {
  vi.useRealTimers();
  globalThis.WebSocket = original;
});

interface Harness {
  stream: EventStream;
  events: GatewayEvent[];
  states: Connection[];
}

function open(): Harness {
  const events: GatewayEvent[] = [];
  const states: Connection[] = [];

  const stream = new EventStream(
    {
      onEvent: (event) => events.push(event),
      onConnectionChange: (state) => states.push(state),
    },
    'ws://gateway/ws',
  );
  stream.start();

  return { stream, events, states };
}

const AN_EVENT: GatewayEvent = {
  kind: 'server.connected',
  at: '2026-08-26T10:00:00Z',
  server: 'files',
};

describe('an open stream', () => {
  it('reports the connection as open once the socket is accepted', () => {
    const { stream, states } = open();
    FakeSocket.latest.accept();

    expect(states).toEqual(['connecting', 'open']);
    stream.stop();
  });

  it('hands each event to the listener', () => {
    const { stream, events } = open();
    FakeSocket.latest.accept();

    FakeSocket.latest.deliver({ type: 'event', event: AN_EVENT });

    expect(events).toEqual([AN_EVENT]);
    stream.stop();
  });

  // The server also sends acknowledgements and errors. Only events are
  // events; treating a welcome as one would put it in the activity feed.
  it('ignores messages that are not events', () => {
    const { stream, events } = open();
    FakeSocket.latest.accept();

    FakeSocket.latest.deliver({ type: 'welcome', kinds: ['tools.changed'] });
    FakeSocket.latest.deliver({ type: 'error', message: 'unknown action' });
    FakeSocket.latest.deliver({ type: 'event' }); // no event body

    expect(events).toEqual([]);
    stream.stop();
  });

  it('survives a message that is not JSON', () => {
    const { stream, events } = open();
    FakeSocket.latest.accept();

    expect(() => FakeSocket.latest.onmessage?.({ data: 'not json' })).not.toThrow();
    FakeSocket.latest.deliver({ type: 'event', event: AN_EVENT });

    expect(events).toEqual([AN_EVENT]);
    stream.stop();
  });

  it('narrows the subscription on request', () => {
    const { stream } = open();
    FakeSocket.latest.accept();

    stream.subscribe(['tools.changed']);

    expect(FakeSocket.latest.sent).toEqual([
      JSON.stringify({ action: 'subscribe', kinds: ['tools.changed'] }),
    ]);
    stream.stop();
  });
});

// The gateway is a local process that gets restarted often. A stream
// that does not come back by itself means a page that silently stops
// updating, which is worse than one that says it is disconnected.
describe('a stream that drops', () => {
  it('reconnects', () => {
    const { stream, states } = open();
    FakeSocket.latest.accept();
    FakeSocket.latest.drop();

    expect(states.at(-1)).toBe('closed');
    expect(FakeSocket.instances).toHaveLength(1);

    vi.advanceTimersByTime(600);

    expect(FakeSocket.instances).toHaveLength(2);
    FakeSocket.latest.accept();
    expect(states.at(-1)).toBe('open');

    stream.stop();
  });

  it('delivers events again after reconnecting', () => {
    const { stream, events } = open();
    FakeSocket.latest.accept();
    FakeSocket.latest.drop();
    vi.advanceTimersByTime(600);
    FakeSocket.latest.accept();

    FakeSocket.latest.deliver({ type: 'event', event: AN_EVENT });

    expect(events).toEqual([AN_EVENT]);
    stream.stop();
  });

  // Backing off matters when the gateway is down rather than
  // restarting: retrying every 500ms for as long as a tab stays open
  // is a busy loop against a machine that is not answering.
  it('waits longer after each failed attempt', () => {
    const { stream } = open();

    // Never accepted: each attempt fails as soon as it is made.
    FakeSocket.latest.drop();
    vi.advanceTimersByTime(500);
    expect(FakeSocket.instances).toHaveLength(2);

    FakeSocket.latest.drop();
    vi.advanceTimersByTime(500);
    expect(FakeSocket.instances).toHaveLength(2); // 1000ms this time

    vi.advanceTimersByTime(500);
    expect(FakeSocket.instances).toHaveLength(3);

    stream.stop();
  });

  // Otherwise a gateway restarted once an hour would, by the end of the
  // day, take fifteen seconds to be noticed as back.
  it('starts backing off from the beginning after a connection that lasted', () => {
    const { stream } = open();

    FakeSocket.latest.drop();
    vi.advanceTimersByTime(500);
    FakeSocket.latest.drop();
    vi.advanceTimersByTime(1000);
    expect(FakeSocket.instances).toHaveLength(3);

    // This one stays up long enough to count as healthy.
    FakeSocket.latest.accept();
    vi.advanceTimersByTime(10_000);
    FakeSocket.latest.drop();

    // So the next attempt is the quick one again.
    vi.advanceTimersByTime(500);
    expect(FakeSocket.instances).toHaveLength(4);

    stream.stop();
  });
});

describe('a stream that is stopped', () => {
  it('does not reconnect', () => {
    const { stream } = open();
    FakeSocket.latest.accept();

    stream.stop();
    vi.advanceTimersByTime(60_000);

    expect(FakeSocket.instances).toHaveLength(1);
  });

  // A component unmounting while a reconnection was already scheduled.
  // Without cancelling the timer, a socket opens for a stream nobody is
  // listening to — and in React's strict mode, where effects run twice,
  // that happens on every mount.
  it('cancels a reconnection that was already pending', () => {
    const { stream } = open();
    FakeSocket.latest.drop();

    stream.stop();
    vi.advanceTimersByTime(60_000);

    expect(FakeSocket.instances).toHaveLength(1);
  });

  it('reports the connection as closed', () => {
    const { stream, states } = open();
    FakeSocket.latest.accept();

    stream.stop();

    expect(states.at(-1)).toBe('closed');
  });
});
