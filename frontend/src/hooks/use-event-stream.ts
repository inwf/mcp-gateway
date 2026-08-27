import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { EventStream, type GatewayEvent } from '@/api/stream';
import { keys } from '@/api/query';
import { useEventStore } from '@/stores/events';

/**
 * Keeps the event stream open for as long as the app is mounted, and
 * turns what arrives into two things: an entry in the activity feed,
 * and the invalidation of whatever the event made stale.
 *
 * Invalidating rather than patching the cache from the event's payload:
 * an event says *that* something changed, and the endpoint is the
 * authority on *what it now is*. Patching would mean maintaining a
 * second, less complete copy of the server's reducer logic here, and
 * any drift between the two shows up as a stale figure on screen that
 * a refresh silently fixes — the hardest kind of bug to be told about.
 */
export function useEventStream(): void {
  const queryClient = useQueryClient();

  useEffect(() => {
    const { push, setConnection } = useEventStore.getState();

    const stream = new EventStream({
      onConnectionChange: setConnection,

      onEvent: (event: GatewayEvent) => {
        push(event);

        switch (event.kind) {
          case 'server.connected':
          case 'server.disconnected':
          case 'server.failed':
          case 'server.status':
            void queryClient.invalidateQueries({ queryKey: keys.servers.all });
            void queryClient.invalidateQueries({ queryKey: keys.gateway.all });
            break;

          case 'tools.changed':
            void queryClient.invalidateQueries({ queryKey: keys.tools.all });
            void queryClient.invalidateQueries({ queryKey: keys.servers.all });
            void queryClient.invalidateQueries({ queryKey: keys.gateway.all });
            break;

          case 'resources.changed':
            void queryClient.invalidateQueries({ queryKey: keys.resources.all });
            void queryClient.invalidateQueries({ queryKey: keys.servers.all });
            break;

          case 'config.updated':
            // A configuration change can add, remove or reconfigure a
            // server, so nothing derived from it can be assumed to hold.
            void queryClient.invalidateQueries();
            break;

          case 'toolcall.started':
          case 'toolcall.completed':
          case 'toolcall.failed':
            // These are for the feed to show. A tool call changes
            // nothing that is on screen elsewhere.
            break;
        }
      },
    });

    stream.start();
    return () => stream.stop();
  }, [queryClient]);
}
