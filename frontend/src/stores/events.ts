import { create } from 'zustand';
import type { Connection, GatewayEvent } from '@/api/stream';

/** How many events are kept for the activity feed. Enough to fill the
 *  dashboard's list several times over; not so many that a busy gateway
 *  grows this without bound over a long session. */
const KEPT = 200;

/** An event with an identity, so a list can key on it. Two events can
 *  share a timestamp — several arrive from one upstream change — so the
 *  timestamp alone will not do. */
export interface FeedEvent extends GatewayEvent {
  id: number;
}

interface EventState {
  connection: Connection;
  events: FeedEvent[];
  /** Counts every event since the page loaded, including ones dropped
   *  from the feed. Also the source of each event's id. */
  seen: number;

  push: (event: GatewayEvent) => void;
  setConnection: (state: Connection) => void;
  clear: () => void;
}

export const useEventStore = create<EventState>((set) => ({
  connection: 'connecting',
  events: [],
  seen: 0,

  push: (event) =>
    set((state) => {
      const id = state.seen + 1;
      // Newest first: the feed reads top-down and the newest line is
      // the one being watched for.
      const events = [{ ...event, id }, ...state.events];
      return {
        seen: id,
        events: events.length > KEPT ? events.slice(0, KEPT) : events,
      };
    }),

  setConnection: (connection) => set({ connection }),

  clear: () => set({ events: [] }),
}));
