import { Button, Result } from 'antd';
import { useTranslation } from 'react-i18next';
import { ApiError } from '@/api/client';

/**
 * A failed query, explained.
 *
 * The gateway being unreachable is separated from every other failure
 * because it is the one the user can act on and the one that happens
 * most: it means the process is not running. Reporting it the same way
 * as a rejected request would send someone looking at their
 * configuration for a problem that is not there.
 */
export function ErrorNotice({
  error,
  onRetry,
}: {
  error: unknown;
  onRetry?: (() => void) | undefined;
}) {
  const { t } = useTranslation();

  const failure = error instanceof ApiError ? error : null;
  const offline = failure?.isOffline ?? false;

  return (
    <Result
      status={offline ? 'warning' : 'error'}
      title={offline ? t('error.offline') : t('error.title')}
      subTitle={
        <span>
          {offline ? t('error.offlineHint') : (failure?.message ?? t('error.unknown'))}
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
        onRetry ? (
          <Button type="primary" onClick={onRetry}>
            {t('error.retry')}
          </Button>
        ) : null
      }
    />
  );
}
