import { Button, Result } from 'antd';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';

export function NotFound() {
  const { t } = useTranslation();
  const navigate = useNavigate();

  return (
    <Result
      status="404"
      title="404"
      subTitle={t('error.notFound')}
      extra={
        <Button type="primary" onClick={() => void navigate('/')}>
          {t('error.back')}
        </Button>
      }
    />
  );
}
