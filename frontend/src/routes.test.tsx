import { describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { RouterProvider, createMemoryRouter } from 'react-router-dom';
import type { RouteObject } from 'react-router-dom';
import { Suspense, lazy, type ComponentType } from 'react';
import { renderWithProviders } from '@/test/harness';
import { PageError } from '@/components/PageError';

/*
 * A page whose code fails to arrive.
 *
 * This is not hypothetical: it happens whenever the gateway is rebuilt
 * while a tab is open, because the chunk the page asks for no longer
 * exists. It has a property that makes it worse than most failures —
 * React's lazy() remembers the rejection, so the page cannot recover by
 * being visited again — and the default handling is a bare stack trace
 * with nothing to click.
 *
 * The behaviour under test is the same as routes.tsx implements: retry
 * once inside the factory, and when that also fails, say what happened
 * and offer the one thing that fixes it.
 */

function lazyPage(load: () => Promise<{ default: ComponentType }>) {
  return lazy(async () => {
    try {
      return await load();
    } catch {
      return await load();
    }
  });
}

function mount(Page: ComponentType) {
  const routes: RouteObject[] = [
    {
      path: '/',
      element: (
        <Suspense fallback={<p>loading</p>}>
          <Page />
        </Suspense>
      ),
      errorElement: <PageError />,
    },
  ];
  renderWithProviders(<RouterProvider router={createMemoryRouter(routes)} />);
}

// A dropped request is the common case, and one retry is all it takes.
// Without it a single network hiccup leaves a page permanently dead,
// because React never calls the factory a second time.
it('recovers from a single failed attempt', async () => {
  const load = vi
    .fn<() => Promise<{ default: ComponentType }>>()
    .mockRejectedValueOnce(new Error('Failed to fetch dynamically imported module'))
    .mockResolvedValueOnce({ default: () => <p>the page</p> });

  mount(lazyPage(load));

  expect(await screen.findByText('the page')).toBeInTheDocument();
  expect(load).toHaveBeenCalledTimes(2);
});

describe('a page whose code is gone for good', () => {
  it('says the code is stale rather than showing a stack trace', async () => {
    const load = vi
      .fn<() => Promise<{ default: ComponentType }>>()
      .mockRejectedValue(new Error('Failed to fetch dynamically imported module'));

    mount(lazyPage(load));

    expect(await screen.findByText('页面代码已过期')).toBeInTheDocument();
    // Reloading is the only thing that fixes it, so it is the action
    // offered — a retry button would do nothing, since the rejection is
    // cached.
    expect(screen.getByRole('button', { name: /重新加载/ })).toBeInTheDocument();
    expect(load).toHaveBeenCalledTimes(2);
  });

  // A page that throws for its own reasons is a different problem and
  // must not be reported as stale code.
  it('reports an ordinary failure as itself', async () => {
    const Boom = () => {
      throw new Error('列表渲染失败');
    };

    mount(Boom);

    expect(await screen.findByText('出错了')).toBeInTheDocument();
    expect(screen.getByText('列表渲染失败')).toBeInTheDocument();
    expect(screen.queryByText('页面代码已过期')).not.toBeInTheDocument();
  });
});
