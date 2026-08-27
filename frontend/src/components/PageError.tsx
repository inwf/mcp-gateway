import { useRouteError } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Button, Result } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { ApiError } from '@/api/client';

/**
 * A page that failed to render, with a way out.
 *
 * This is a route-level error element rather than a boundary around the
 * whole application, so the rail survives: the other pages still work,
 * and navigating away is one click.
 *
 * The case worth singling out is a page's code failing to load. It
 * happens for a mundane reason — the gateway was rebuilt while this tab
 * was open, so the chunk this page asked for no longer exists — and it
 * has a specific, non-obvious property: React's lazy() caches the
 * rejection, so the page cannot recover by being visited again. Nothing
 * short of reloading the document will fix it, which is why that is the
 * action offered rather than a retry.
 */

/** Whether the failure is a module that could not be fetched. The
 *  wording differs between browsers, so all three forms are matched. */
function isStaleCode(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error);
  return (
    message.includes('dynamically imported module') ||
    message.includes('Importing a module script failed') ||
    message.includes('Failed to fetch')
  );
}

export function PageError() {
  const { t } = useTranslation();
  const error = useRouteError();

  if (isStaleCode(error)) {
    return (
      <Result
        status="warning"
        title={t('error.staleCode')}
        subTitle={t('error.staleCodeHint')}
        extra={
          <Button
            type="primary"
            icon={<ReloadOutlined aria-hidden />}
            onClick={() => window.location.reload()}
          >
            {t('error.reload')}
          </Button>
        }
      />
    );
  }

  const failure = error instanceof ApiError ? error : null;
  const message = failure?.message ?? (error instanceof Error ? error.message : null);

  return (
    <Result
      status="error"
      title={t('error.title')}
      subTitle={
        <span>
          {message ?? t('error.unknown')}
          {failure?.requestId ? (
            <>
              <br />
              <span className="mono" style={{ fontSize: 'var(--text-xs)', opacity: 0.7 }}>
                {t('error.requestId')} {failure.requestId}
              </span>
            </>
          ) : null}
        </span>
      }
      extra={
        <Button icon={<ReloadOutlined aria-hidden />} onClick={() => window.location.reload()}>
          {t('error.reload')}
        </Button>
      }
    />
  );
}
