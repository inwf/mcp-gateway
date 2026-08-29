import { useMemo } from 'react';
import { App as AntApp, ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createBrowserRouter } from 'react-router-dom';
import { antdThemeFor } from '@/theme/antd';
import { createQueryClient } from '@/api/query';
import { routes } from '@/routes';
import { useEventStream } from '@/hooks/use-event-stream';
import { useTheme } from '@/hooks/use-theme';

/** Holds the event stream open for the life of the app. It has to sit
 *  inside the query provider, since what it does with an event is
 *  invalidate queries. */
function StreamKeeper({ children }: { children: React.ReactNode }) {
  useEventStream();
  return children;
}

export function App() {
  // Created once. A client rebuilt on render would throw away every
  // cached response on each state change.
  const queryClient = useMemo(() => createQueryClient(), []);
  const router = useMemo(() => createBrowserRouter(routes), []);
  const mode = useTheme();

  return (
    <ConfigProvider
      theme={antdThemeFor(mode)}
      locale={zhCN}
      // antd puts a space between the two characters of a Chinese
      // button label by convention. On a dense console it reads as a
      // typo rather than as typography.
      button={{ autoInsertSpace: false }}
    >
      {/* AntApp is what makes message and modal calls pick up the theme
          above; the static antd.message.* functions render outside this
          tree and would come out in the default light palette.

          The height is not decoration. AntApp renders a div, and that div
          sits between #root and the shell — so without a height of its own
          it breaks the chain from html down, the shell's height: 100%
          resolves against nothing and collapses to its content, and the
          window is left part empty. Worse, the shell then never overflows,
          so the pane inside it that is supposed to scroll never does:
          anything past the bottom of the window is clipped by the body and
          cannot be reached at all. */}
      <AntApp style={{ height: '100%' }}>
        <QueryClientProvider client={queryClient}>
          <StreamKeeper>
            <div className="field" aria-hidden="true" />
            <RouterProvider router={router} />
          </StreamKeeper>
        </QueryClientProvider>
      </AntApp>
    </ConfigProvider>
  );
}
