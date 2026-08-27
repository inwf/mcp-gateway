import { Spin } from 'antd';
import { useTranslation } from 'react-i18next';

/** Shown while a lazily-loaded page is fetched. Centred in whatever
 *  space it is given rather than pinned to the viewport, so it appears
 *  where the page will. */
export function PageSpinner() {
  const { t } = useTranslation();

  return (
    <div
      style={{
        display: 'grid',
        placeItems: 'center',
        minHeight: '40vh',
      }}
      role="status"
      aria-label={t('common.loading')}
    >
      <Spin />
    </div>
  );
}
