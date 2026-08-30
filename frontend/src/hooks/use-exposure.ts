import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { App } from 'antd';
import { endpoints } from '@/api/endpoints';
import { keys } from '@/api/query';
import type { MCPServer } from '@/api/types';

/**
 * Turning one tool's exposure on and off.
 *
 * Exposure is a property of the server's configuration, so a change here
 * is a configuration write — the same call the edit form makes. The whole
 * allow list goes with it, because that is what the endpoint takes, and
 * because a list built from what was last read is what keeps two tabs
 * from silently dropping each other's changes.
 *
 * Deliberately not optimistic. The exposed name does not exist until the
 * gateway has republished its tool list, and guessing it here would be a
 * second place computing those names — which is the fault this whole area
 * was just repaired for.
 *
 * Lives in one place because two pages offer the switch: the tools page,
 * where someone browsing decides what a client should see, and a server's
 * own tool list. Two copies would drift on which queries to invalidate,
 * and the symptom would be a page that shows a stale answer.
 */
export function useExposure() {
  const queryClient = useQueryClient();
  const { message } = App.useApp();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: ({
      server,
      config,
      tool,
      on,
    }: {
      server: string;
      config: MCPServer;
      tool: string;
      on: boolean;
    }) => {
      const current = config.exposedTools ?? [];
      const next = on
        ? [...new Set([...current, tool])]
        : current.filter((name) => name !== tool);
      return endpoints.updateServer(server, { ...config, exposedTools: next });
    },
    onSuccess: () => {
      // The tool list, the servers and what the gateway publishes all
      // change together, and all three are on screen somewhere.
      void queryClient.invalidateQueries({ queryKey: keys.servers.all });
      void queryClient.invalidateQueries({ queryKey: keys.tools.all });
      void queryClient.invalidateQueries({ queryKey: keys.gateway.all });
    },
    onError: (error: unknown) => {
      message.error(error instanceof Error ? error.message : t('error.unknown'));
    },
  });
}
