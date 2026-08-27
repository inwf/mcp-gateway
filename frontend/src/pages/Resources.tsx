import { useTranslation } from 'react-i18next';

// Placeholder. The page itself lands in the next batch; this exists so
// that routing, the shell and the data layer can be exercised end to
// end before any of it depends on a finished page.
export default function Resources() {
  const { t } = useTranslation();
  return <p className="label">{t('resources.title')}</p>;
}
