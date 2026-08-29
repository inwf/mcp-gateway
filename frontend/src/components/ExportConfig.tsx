import { useTranslation } from 'react-i18next';
import { Button, Dropdown, Tooltip } from 'antd';
import { DownloadOutlined } from '@ant-design/icons';

/**
 * Downloading the configuration.
 *
 * Two downloads, not one with a hidden choice. A configuration meant for
 * a bug report must not carry an API token; one meant as a backup is
 * useless without them. Whichever were the default, the other use would
 * silently get the wrong file — so both are named on the menu and neither
 * happens by accident.
 *
 * It is an ordinary link rather than a fetch-and-blob: the gateway
 * already sends the filename in a Content-Disposition header, and letting
 * the browser do the saving means no copy of the secrets passes through
 * this page's memory.
 */

const EXPORT = '/api/config/export';

export function ExportConfig() {
  const { t } = useTranslation();

  return (
    <Dropdown
      trigger={['click']}
      menu={{
        items: [
          {
            key: 'redacted',
            label: (
              <a href={EXPORT} download>
                {t('settings.exportRedacted')}
              </a>
            ),
          },
          {
            key: 'secrets',
            danger: true,
            label: (
              <Tooltip title={t('settings.exportSecretsHint')} placement="left">
                <a href={`${EXPORT}?secrets=true`} download>
                  {t('settings.exportSecrets')}
                </a>
              </Tooltip>
            ),
          },
        ],
      }}
    >
      <Button icon={<DownloadOutlined aria-hidden />}>{t('settings.export')}</Button>
    </Dropdown>
  );
}
