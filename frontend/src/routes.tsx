/* oxlint-disable react/only-export-components --
 * This file's export is the route table, and the lazily-loaded pages
 * have to be declared where the table can reach them. Splitting them
 * out to satisfy a fast-refresh heuristic would put the list of pages
 * in one file and the list of paths in another, which is worse to read
 * for a benefit that only applies while editing this file.
 */
import { Suspense, lazy } from 'react';
import { Navigate, type RouteObject } from 'react-router-dom';
import { Shell } from '@/layout/Shell';
import { PageSpinner } from '@/components/PageSpinner';
import { NotFound } from '@/pages/NotFound';

/*
 * Pages are loaded on demand. The whole bundle ships inside the gateway
 * binary and is served from loopback, so this is not about download
 * time — it is about the first paint not waiting on the YAML editor and
 * the JSON viewer, which are the two heaviest things here and are on
 * pages most visits never open.
 */
const Overview = lazy(() => import('@/pages/Overview'));
const Servers = lazy(() => import('@/pages/Servers'));
const ServerDetail = lazy(() => import('@/pages/ServerDetail'));
const Tools = lazy(() => import('@/pages/Tools'));
const Resources = lazy(() => import('@/pages/Resources'));
const Logs = lazy(() => import('@/pages/Logs'));
const Settings = lazy(() => import('@/pages/Settings'));

function page(element: React.ReactNode) {
  return <Suspense fallback={<PageSpinner />}>{element}</Suspense>;
}

export const routes: RouteObject[] = [
  {
    path: '/',
    element: <Shell />,
    children: [
      { index: true, element: page(<Overview />) },

      { path: 'servers', element: page(<Servers />) },
      // The detail page keeps its tab in the URL, so a link to a
      // server's logs is a link someone can send.
      { path: 'servers/:name', element: <Navigate to="overview" replace /> },
      { path: 'servers/:name/:tab', element: page(<ServerDetail />) },

      { path: 'tools', element: page(<Tools />) },
      { path: 'resources', element: page(<Resources />) },
      { path: 'logs', element: page(<Logs />) },
      { path: 'settings', element: page(<Settings />) },

      { path: '*', element: <NotFound /> },
    ],
  },
];
