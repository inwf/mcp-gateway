import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { setupServer } from 'msw/node';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from '@/test/harness';
import type { MCPServer } from '@/api/types';
import { readPastedServer, withServerDefaults } from '@/lib/server-json';
import { ServerForm } from './ServerForm';

const api = setupServer();
beforeAll(() => api.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => api.resetHandlers());
afterAll(() => api.close());

// The form is quicker for the usual case, but it cannot express a
// configuration someone already has in hand — and it cannot show exactly
// what will be sent. The JSON view is both of those.

const EXISTING: MCPServer = {
  transport: 'stdio',
  enabled: true,
  timeout: '1m0s',
  command: 'npx',
  args: ['-y', 'server'],
};

function open(editing?: { name: string; server: MCPServer }) {
  renderWithProviders(<ServerForm open onClose={() => {}} editing={editing} />);
}

const toJSON = () => userEvent.click(screen.getByTitle('JSON'));
const toForm = () => userEvent.click(screen.getByTitle('表单'));

function jsonBox(): HTMLTextAreaElement {
  const boxes = screen.getAllByRole('textbox');
  const found = boxes.find((box) => box.tagName === 'TEXTAREA' && box.classList.contains('mono'));
  if (!found) throw new Error('the JSON editor is not on screen');
  return found as HTMLTextAreaElement;
}

describe('reading pasted JSON', () => {
  it('takes a bare server configuration', () => {
    expect(readPastedServer('{"command":"npx"}')).toEqual({ server: { command: 'npx' } });
  });

  // The configuration file and every other client's file wrap it, so a
  // wrapper is what someone is most likely to have copied.
  it('unwraps one written the way a configuration file writes it', () => {
    expect(readPastedServer('{"mcpServers":{"files":{"command":"npx"}}}')).toEqual({
      name: 'files',
      server: { command: 'npx' },
    });
  });

  // Several servers is a document, and importing documents is what the
  // import dialog is for. Taking the first would silently drop the rest.
  it('refuses a wrapper holding more than one server', () => {
    expect(() =>
      readPastedServer('{"mcpServers":{"a":{"command":"x"},"b":{"command":"y"}}}'),
    ).toThrow('jsonOneServer');
  });

  it('refuses something that is not an object', () => {
    expect(() => readPastedServer('[1,2]')).toThrow('jsonNotAnObject');
    expect(() => readPastedServer('"text"')).toThrow('jsonNotAnObject');
  });

  it('reports where the JSON went wrong', () => {
    expect(() => readPastedServer('{oops')).toThrow(/.+/);
  });

  it('rejects legacy server tags while preserving an upstream environment field with that name', () => {
    for (const text of [
      '{"command":"npx","tags":{}}',
      '{"mcpServers":{"files":{"command":"npx","tags":{"env":"dev"}}}}',
    ]) {
      expect(() => readPastedServer(text)).toThrow('jsonServerTagsRemoved');
    }
    expect(readPastedServer('{"command":"npx","env":{"tags":"business"}}')).toEqual({
      server: { command: 'npx', env: { tags: 'business' } },
    });
  });
});

// Pasted JSON is allowed to be as short as the gateway allows, and the
// form has an input for every field.
describe('filling in what was left out', () => {
  it('takes a command to mean a child process', () => {
    expect(withServerDefaults({ command: 'npx' } as MCPServer)).toMatchObject({
      transport: 'stdio',
      enabled: true,
      timeout: '1m0s',
    });
  });

  it('takes a url to mean a server running elsewhere', () => {
    expect(withServerDefaults({ url: 'https://example.com/mcp' } as MCPServer)).toMatchObject({
      transport: 'streamable-http',
    });
  });

  it('leaves what was given alone', () => {
    expect(
      withServerDefaults({ transport: 'stdio', enabled: false, timeout: '5s' } as MCPServer),
    ).toMatchObject({ enabled: false, timeout: '5s' });
  });
});

describe('the two views', () => {
  it('shows the server being edited as JSON', async () => {
    open({ name: 'files', server: EXISTING });
    await toJSON();

    const shown = JSON.parse(jsonBox().value) as MCPServer;
    expect(shown).toMatchObject({ transport: 'stdio', command: 'npx', args: ['-y', 'server'] });
  });

  // They are two views of one server, not two forms: whatever is entered
  // in one has to be there in the other.
  it('carries what was typed in the form into the JSON', async () => {
    open();
    await userEvent.type(screen.getByLabelText('命令'), 'uvx');
    await toJSON();

    expect(JSON.parse(jsonBox().value)).toMatchObject({ command: 'uvx' });
  });

  it('carries what was typed in the JSON back into the form', async () => {
    open({ name: 'files', server: EXISTING });
    await toJSON();

    await userEvent.clear(jsonBox());
    await userEvent.paste('{"transport":"streamable-http","url":"https://example.com/mcp"}');
    await toForm();

    await waitFor(() =>
      expect(screen.getByLabelText('地址')).toHaveValue('https://example.com/mcp'),
    );
  });

  // The text is the work. Switching away from JSON that cannot be read
  // would drop it, so the switch is refused and the reason shown.
  it('refuses to leave JSON that cannot be read', async () => {
    open();
    await toJSON();

    await userEvent.clear(jsonBox());
    await userEvent.paste('{not json');
    await toForm();

    expect(await screen.findByText(/不是合法的 JSON/)).toBeInTheDocument();
    // Still in the JSON view, with the text intact.
    expect(jsonBox().value).toBe('{not json');
  });
});

describe('saving from the JSON view', () => {
  it('sends what was typed', async () => {
    let received: unknown;
    api.use(
      http.put('/api/servers/files', async ({ request }) => {
        received = await request.json();
        return HttpResponse.json({});
      }),
    );

    open({ name: 'files', server: EXISTING });
    await toJSON();
    await userEvent.clear(jsonBox());
    await userEvent.paste('{"transport":"stdio","enabled":true,"timeout":"30s","command":"uvx"}');
    await userEvent.click(screen.getByRole('button', { name: '保存' }));

    await waitFor(() =>
      expect(received).toEqual({
        server: { transport: 'stdio', enabled: true, timeout: '30s', command: 'uvx' },
      }),
    );
  });

  // A wrapper carries the name, which is the one thing a bare
  // configuration cannot supply when adding.
  it('takes the name from a wrapper when adding', async () => {
    let path = '';
    let received: unknown;
    api.use(
      http.post('/api/servers', async ({ request }) => {
        path = new URL(request.url).pathname;
        received = await request.json();
        return HttpResponse.json({}, { status: 201 });
      }),
    );

    open();
    await toJSON();
    await userEvent.clear(jsonBox());
    await userEvent.paste('{"mcpServers":{"pasted":{"command":"npx"}}}');
    await userEvent.click(screen.getByRole('button', { name: '创建' }));

    await waitFor(() => expect(path).toBe('/api/servers'));
    expect(received).toMatchObject({ name: 'pasted' });
  });
});

// The name identifies the server and is not part of what the JSON view
// edits, so it stays reachable while that view is showing.
describe('the name', () => {
  it('is still on screen in the JSON view', async () => {
    open();
    await toJSON();

    expect(within(screen.getByRole('dialog')).getByText('名称')).toBeInTheDocument();
  });
});
