import { useMutation, useQueryClient } from '@tanstack/react-query';
import { App } from 'antd';
import { useTranslation } from 'react-i18next';
import { endpoints } from '@/api/endpoints';
import { keys } from '@/api/query';
import { ApiError } from '@/api/client';
import type { MCPServer } from '@/api/types';

/**
 * The things one can do to a server, with their consequences.
 *
 * Every one of these changes something the whole application shows, so
 * they invalidate rather than patch — the same reasoning as for the
 * event stream. The event stream will usually invalidate these too;
 * doing it here as well is what makes the UI correct when the stream is
 * down, which is exactly when someone is most likely to be clicking
 * "connect".
 */
export function useServerActions() {
  const { t } = useTranslation();
  const { message } = App.useApp();
  const queryClient = useQueryClient();

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: keys.servers.all });
    void queryClient.invalidateQueries({ queryKey: keys.gateway.all });
    void queryClient.invalidateQueries({ queryKey: keys.tools.all });
    void queryClient.invalidateQueries({ queryKey: keys.resources.all });
  };

  const report = (error: unknown) => {
    const failure = error instanceof ApiError ? error : null;
    void message.error(failure?.message ?? t('error.unknown'));
  };

  const connect = useMutation({
    mutationFn: (name: string) => endpoints.connectServer(name),
    onSuccess: refresh,
    onError: report,
  });

  const disconnect = useMutation({
    mutationFn: (name: string) => endpoints.disconnectServer(name),
    onSuccess: refresh,
    onError: report,
  });

  const remove = useMutation({
    mutationFn: (name: string) => endpoints.deleteServer(name),
    onSuccess: refresh,
    onError: report,
  });

  const create = useMutation({
    mutationFn: ({ name, server }: { name: string; server: MCPServer }) =>
      endpoints.createServer(name, server),
    onSuccess: refresh,
  });

  const update = useMutation({
    mutationFn: ({ name, server }: { name: string; server: MCPServer }) =>
      endpoints.updateServer(name, server),
    onSuccess: refresh,
  });

  return { connect, disconnect, remove, create, update };
}
