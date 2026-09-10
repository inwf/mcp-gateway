import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import type { ServerState, ServerView } from '@/api/types';
import { renderWithProviders } from '@/test/harness';
import Servers from './Servers';

function server(name: string, state: ServerState, tags: Record<string, string>): ServerView {
  return {
    name,
    config: { transport: 'stdio', enabled: true, command: 'node', timeout: '30s', tags },
    status: {
      name,
      state,
      toolCount: 2,
      resourceCount: 0,
      hasTools: true,
      hasResources: false,
      hasPrompts: false,
      hasLogging: false,
    },
    exposedCount: 2,
  };
}

const api = setupServer(
  http.get('/api/servers', () =>
    HttpResponse.json({
      servers: [
        server('files', 'connected', { env: 'prod', team: 'core' }),
        server('reports', 'failed', { env: 'prod', team: 'ops' }),
        server('archive', 'disconnected', { env: 'dev', team: 'core' }),
      ],
    }),
  ),
);

beforeAll(() => api.listen({ onUnhandledRequest: 'error' }));
afterEach(() => api.resetHandlers());
afterAll(() => api.close());

function visit() {
  renderWithProviders(
    <MemoryRouter>
      <Servers />
    </MemoryRouter>,
  );
}

describe('finding servers', () => {
  it('combines the connection filter with search and can restore the full inventory', async () => {
    const user = userEvent.setup();
    visit();
    await screen.findByRole('link', { name: 'files' });

    await user.click(screen.getByRole('button', { name: /^连接失败/ }));
    expect(screen.getByRole('link', { name: 'reports' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'files' })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'archive' })).not.toBeInTheDocument();

    await user.type(screen.getByRole('searchbox', { name: '搜索服务器' }), 'files');
    expect(screen.getByText('没有匹配的服务器')).toBeInTheDocument();

    await user.click(screen.getAllByRole('button', { name: '清除筛选' })[0]!);
    for (const name of ['files', 'reports', 'archive']) {
      expect(screen.getByRole('link', { name })).toBeInTheDocument();
    }
    expect(screen.getByRole('searchbox')).toHaveValue('');
    expect(screen.getByRole('button', { name: /^全部/ })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
  });

  it('keeps selected tags removable when a status filter leaves no matching row', async () => {
    const user = userEvent.setup();
    visit();
    const link = await screen.findByRole('link', { name: 'files' });
    // The name identifies the row without computing accessible names for
    // every action and tooltip in the entire table.
    const files = link.closest('tr');
    if (!files) throw new Error('the server is not in an inventory row');
    await user.click(within(files).getByRole('button', { name: 'env=prod' }));
    await user.click(within(files).getByRole('button', { name: 'team=core' }));
    expect(screen.queryByRole('link', { name: 'reports' })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'archive' })).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /^连接失败/ }));
    expect(screen.getByText('没有匹配的服务器')).toBeInTheDocument();

    // A tag must remain removable after the row that supplied it is hidden.
    await user.click(screen.getByRole('button', { name: '移除标签筛选 team=core' }));
    expect(screen.getByRole('link', { name: 'reports' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'archive' })).not.toBeInTheDocument();
  });
});
