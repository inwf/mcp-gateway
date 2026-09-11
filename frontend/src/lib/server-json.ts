import type { MCPServer, Transport } from '@/api/types';

/**
 * Reads a server out of pasted JSON.
 *
 * Two shapes are accepted, because both are things people have in hand:
 * the server's configuration on its own, and one wrapped the way the
 * configuration file and every other client's file writes it —
 * {"mcpServers": {"name": {...}}}. The wrapper carries a name, which is
 * used when adding.
 *
 * A wrapper holding several servers is refused rather than half-applied:
 * that is a document, and importing documents is what the import dialog
 * is for.
 */
export function readPastedServer(text: string): { name?: string; server: MCPServer } {
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch (error) {
    throw new Error((error as Error).message);
  }

  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('jsonNotAnObject');
  }

  const wrapper = (parsed as { mcpServers?: unknown }).mcpServers;
  if (wrapper !== undefined) {
    if (wrapper === null || typeof wrapper !== 'object' || Array.isArray(wrapper)) {
      throw new Error('jsonNotAnObject');
    }
    const entries = Object.entries(wrapper as Record<string, unknown>);
    if (entries.length !== 1) throw new Error('jsonOneServer');

    const [name, server] = entries[0]!;
    if (server === null || typeof server !== 'object' || Array.isArray(server)) {
      throw new Error('jsonNotAnObject');
    }
    return { name, server: checkedServer(server) };
  }

  return { server: checkedServer(parsed) };
}

function checkedServer(value: object): MCPServer {
  // Switching back to the form would otherwise silently discard this
  // removed field before the backend's strict parser can report it.
  if (Object.hasOwn(value, 'tags')) throw new Error('jsonServerTagsRemoved');
  return value as MCPServer;
}

/**
 * Fills in what the gateway would have defaulted.
 *
 * Pasted JSON is allowed to be as short as {"command": "npx"} — that is
 * what other clients write, and what the gateway itself accepts. The form
 * has an input for every field, so it needs the whole value.
 */
export function withServerDefaults(server: MCPServer): MCPServer {
  const transport: Transport =
    server.transport ?? (server.url !== undefined ? 'streamable-http' : 'stdio');

  return {
    ...server,
    transport,
    enabled: server.enabled ?? true,
    timeout: server.timeout ?? '1m0s',
  };
}
