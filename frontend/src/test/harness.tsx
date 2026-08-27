import type { ReactNode } from 'react';
import { render } from '@testing-library/react';
import { App as AntApp, ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import { QueryClientProvider } from '@tanstack/react-query';
import '@/i18n';
import { antdTheme } from '@/theme/antd';
import { createQueryClient } from '@/api/query';

/**
 * Renders under the same providers the application uses.
 *
 * The theme and the locale are part of what a component renders — the
 * button-label spacing, the date format, the palette a message picks up.
 * Rendering a component under bare defaults would test a configuration
 * that never ships.
 */
export function renderWithProviders(ui: ReactNode) {
  return render(
    <ConfigProvider theme={antdTheme} locale={zhCN} button={{ autoInsertSpace: false }}>
      <AntApp>
        <QueryClientProvider client={createQueryClient()}>{ui}</QueryClientProvider>
      </AntApp>
    </ConfigProvider>,
  );
}
