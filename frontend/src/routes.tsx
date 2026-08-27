/* oxlint-disable react/only-export-components --
 * This file's export is the route table, and the lazily-loaded pages
 * have to be declared where the table can reach them. Splitting them
 * out to satisfy a fast-refresh heuristic would put the list of pages
 * in one file and the list of paths in another, which is worse to read
 * for a benefit that only applies while editing this file.
 */
import { Suspense, lazy, type ComponentType, type ReactNode } from 'react';
import { Navigate, type RouteObject } from 'react-router-dom';
import { Shell } from '@/layout/Shell';
import { PageSpinner } from '@/components/PageSpinner';
import { PageError } from '@/components/PageError';
import { NotFound } from '@/pages/NotFound';

/*
 * Pages are loaded on demand. The whole bundle ships inside the gateway
 * binary and is served from loopback, so this is not about download
 * time — it is about the first paint not waiting on the YAML editor and
 * the JSON viewer, which are the two heaviest things here and are on
 * pages most visits never open.
 */

/**
 * A lazily-loaded page that survives one failed attempt.
 *
 * React's lazy() calls its factory once and remembers the outcome,
 * including a rejection: a page whose code failed to arrive stays broken
 * for the rest of the session, however many times it is navigated to.
 * That turns a dropped request into a dead page.
 *
 * So the retry happens inside the factory, where React sees a single
 * pending promise and never learns that the first attempt failed. One
 * retry, because the two causes are quite different: a transient
 * failure succeeds on the second attempt, and a chunk that is genuinely
 * gone — the binary was rebuilt while this tab was open — will never
 * succeed, and reloading the document is the only real fix. That case
 * is what PageError offers.
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

const Overview = lazyPage(() => import('@/pages/Overview'));
const Servers = lazyPage(() => import('@/pages/Servers'));
const ServerDetail = lazyPage(() => import('@/pages/ServerDetail'));
const Tools = lazyPage(() => import('@/pages/Tools'));
const Resources = lazyPage(() => import('@/pages/Resources'));
const Logs = lazyPage(() => import('@/pages/Logs'));
const Settings = lazyPage(() => import('@/pages/Settings'));

/** Wraps a page in its loading fallback. */
function page(element: ReactNode): ReactNode {
  return <Suspense fallback={<PageSpinner />}>{element}</Suspense>;
}

/* Every page carries its own error element rather than there being one
 * around the application, so that a page which fails leaves the rail —
 * and therefore every other page — reachable. */
const onError = { errorElement: <PageError /> };

export const routes: RouteObject[] = [
  {
    path: '/',
    element: <Shell />,
    errorElement: <PageError />,
    children: [
      { index: true, element: page(<Overview />), ...onError },

      { path: 'servers', element: page(<Servers />), ...onError },
      // The detail page keeps its tab in the URL, so a link to a
      // server's logs is a link someone can send.
      { path: 'servers/:name', element: <Navigate to="overview" replace /> },
      { path: 'servers/:name/:tab', element: page(<ServerDetail />), ...onError },

      { path: 'tools', element: page(<Tools />), ...onError },
      { path: 'resources', element: page(<Resources />), ...onError },
      { path: 'logs', element: page(<Logs />), ...onError },
      { path: 'settings', element: page(<Settings />), ...onError },

      { path: '*', element: <NotFound /> },
    ],
  },
];
