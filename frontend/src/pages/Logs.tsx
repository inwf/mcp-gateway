import { useTranslation } from 'react-i18next';
import { Panel } from '@/components/Panel';
import { PageHeading } from '@/components/PageHeading';
import { LogView } from '@/components/LogView';
import styles from './Logs.module.css';

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
    <div className={styles.page}>
      <PageHeading title={t('logs.title')} description={t('logs.description')} />
      <Panel title={t('logs.stream')} className={styles.panel}>
        <LogView />
      </Panel>
    </div>
  );
}
