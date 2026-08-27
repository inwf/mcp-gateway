import { useTranslation } from 'react-i18next';
import { Panel } from '@/components/Panel';
import { LogView } from '@/components/LogView';

/**
 * The whole log.
 *
 * Everything here lives in LogView, which the server detail page also
 * uses. The difference between the two is which filters are fixed: this
 * one lets the server be chosen, that one pins it.
 */
export default function Logs() {
  const { t } = useTranslation();

  return (
    <Panel title={t('logs.title')}>
      <LogView />
    </Panel>
  );
}
