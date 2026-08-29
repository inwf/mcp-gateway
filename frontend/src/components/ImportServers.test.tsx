import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { setupServer } from 'msw/node';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from '@/test/harness';
import { ImportServers } from './ImportServers';

const api = setupServer();
beforeAll(() => api.listen({ onUnhandledRequest: 'error' }));
afterEach(() => api.resetHandlers());
afterAll(() => api.close());

const DOCUMENT = '{"mcpServers":{"files":{"command":"npx"}}}';

async function paste(text: string) {
  const editor = screen.getByRole('textbox');
  await userEvent.click(editor);
  await userEvent.paste(text);
}

async function run() {
  await userEvent.click(screen.getByRole('button', { name: '导入' }));
}

function open() {
  renderWithProviders(<ImportServers open onClose={() => {}} />);
}

// The document is another program's configuration file, sent exactly as
// pasted: which fields need translating is the gateway's business, so
// that the same file works through curl as through here.
describe('what gets sent', () => {
  it('posts the pasted document as it is', async () => {
    let received: unknown;
    api.use(
      http.post('/api/servers/import', async ({ request }) => {
        received = await request.json();
        return HttpResponse.json({ results: [{ name: 'files' }], imported: 1, failed: 0 });
      }),
    );

    open();
    await paste(DOCUMENT);
    await run();

    await waitFor(() => expect(received).toEqual({ mcpServers: { files: { command: 'npx' } } }));
  });

  it('will not send something that is not JSON', async () => {
    open();
    await paste('{not json');

    expect(screen.getByRole('button', { name: '导入' })).toBeDisabled();
  });
});

// Nine good servers and one typo leaves nine configured and one message
// about the tenth — so both halves have to be on screen. Reporting only
// the failures would leave someone wondering what did land; reporting
// only a count would not say which.
describe('what comes back', () => {
  it('names what was imported and what was not', async () => {
    api.use(
      http.post('/api/servers/import', () =>
        HttpResponse.json({
          results: [
            { name: 'alpha' },
            { name: 'beta' },
            { name: 'broken', error: 'transport is "carrier-pigeon"' },
          ],
          imported: 2,
          failed: 1,
        }),
      ),
    );

    open();
    await paste(DOCUMENT);
    await run();

    expect(await screen.findByText('alpha')).toBeInTheDocument();
    expect(screen.getByText('beta')).toBeInTheDocument();
    expect(screen.getByText('broken')).toBeInTheDocument();
    expect(screen.getByText('transport is "carrier-pigeon"')).toBeInTheDocument();
  });

  // A document from which nothing could be imported is a failed request,
  // and the envelope still carries one field error per entry. Showing
  // only the summary sentence would drop the reasons, which are the part
  // that makes it fixable.
  it('shows why each entry failed when none could be imported', async () => {
    api.use(
      http.post('/api/servers/import', () =>
        HttpResponse.json(
          {
            error: {
              code: 'validation_failed',
              message: 'no server in the document could be imported',
              fields: [{ field: 'files', message: 'a server named "files" already exists' }],
            },
          },
          { status: 422 },
        ),
      ),
    );

    open();
    await paste(DOCUMENT);
    await run();

    expect(await screen.findByText('files')).toBeInTheDocument();
    expect(
      screen.getByText('a server named "files" already exists'),
    ).toBeInTheDocument();
  });

  // A failure with no per-entry detail — the gateway unreachable, say —
  // still has to say something rather than nothing.
  it('reports a failure that names no entry', async () => {
    api.use(
      http.post('/api/servers/import', () =>
        HttpResponse.json(
          { error: { code: 'bad_request', message: 'the document has no mcpServers in it' } },
          { status: 400 },
        ),
      ),
    );

    open();
    await paste(DOCUMENT);
    await run();

    expect(await screen.findByText('the document has no mcpServers in it')).toBeInTheDocument();
  });
});
