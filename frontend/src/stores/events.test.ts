import { beforeEach, describe, expect, it } from 'vitest';
import { useEventStore } from './events';
import type { GatewayEvent } from '@/api/stream';

function event(kind: GatewayEvent['kind'], server?: string): GatewayEvent {
  return { kind, at: '2026-08-26T10:00:00Z', ...(server ? { server } : {}) };
}

beforeEach(() => {
  useEventStore.setState({ connection: 'connecting', events: [], seen: 0 });
});

describe('the event feed', () => {
  it('keeps what arrives', () => {
    useEventStore.getState().push(event('server.connected', 'files'));

    const { events } = useEventStore.getState();
    expect(events).toHaveLength(1);
    expect(events[0]?.kind).toBe('server.connected');
    expect(events[0]?.server).toBe('files');
  });

  // The feed is read top-down and the line being watched for is the
  // newest one.
  it('puts the newest first', () => {
    const store = useEventStore.getState();
    store.push(event('server.connected', 'first'));
    store.push(event('tools.changed', 'second'));

    expect(useEventStore.getState().events.map((e) => e.server)).toEqual(['second', 'first']);
  });

  // Two events from one upstream change share a timestamp, so a list
  // keyed on time would collide. Ids are what a rendered list keys on.
  it('gives each event an identity of its own', () => {
    const store = useEventStore.getState();
    store.push(event('tools.changed', 'files'));
    store.push(event('tools.changed', 'files'));

    const ids = useEventStore.getState().events.map((e) => e.id);
    expect(new Set(ids).size).toBe(2);
  });

  // A gateway left running for a day must not grow this without bound.
  it('drops the oldest once it is full', () => {
    const store = useEventStore.getState();
    for (let i = 0; i < 260; i++) {
      store.push({ kind: 'tools.changed', at: '2026-08-26T10:00:00Z', server: `s${i}` });
    }

    const { events, seen } = useEventStore.getState();
    expect(events).toHaveLength(200);
    expect(events[0]?.server).toBe('s259');
    // The count is of everything that happened, not of what was kept.
    expect(seen).toBe(260);
  });

  it('can be emptied without losing the count', () => {
    const store = useEventStore.getState();
    store.push(event('config.updated'));
    useEventStore.getState().clear();

    expect(useEventStore.getState().events).toEqual([]);
    expect(useEventStore.getState().seen).toBe(1);
  });
});

describe('the connection state', () => {
  it('is what was last reported', () => {
    useEventStore.getState().setConnection('open');
    expect(useEventStore.getState().connection).toBe('open');

    useEventStore.getState().setConnection('closed');
    expect(useEventStore.getState().connection).toBe('closed');
  });
});
