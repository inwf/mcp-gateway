import { useTranslation } from 'react-i18next';
import { Segmented, Tooltip } from 'antd';
import { DesktopOutlined, MoonOutlined, SunOutlined } from '@ant-design/icons';
import { useThemeStore, type ThemeChoice } from '@/stores/theme';

/**
 * Choosing the theme.
 *
 * It lives in the top bar rather than on the settings page, which is
 * where the old interface kept it. This is a display preference: the way
 * to decide between two themes is to flip between them and look, and that
 * is not something to do by navigating away from whatever you were
 * reading. Everything else on the settings page changes what the gateway
 * does; this changes only what the screen looks like.
 *
 * Three choices, because "follow the system" is a real answer and not the
 * same as either of the other two — someone whose machine switches at
 * dusk wants this to switch with it.
 */

const CHOICES: Array<{ value: ThemeChoice; labelKey: string; Icon: typeof SunOutlined }> = [
  { value: 'light', labelKey: 'theme.light', Icon: SunOutlined },
  { value: 'dark', labelKey: 'theme.dark', Icon: MoonOutlined },
  { value: 'system', labelKey: 'theme.system', Icon: DesktopOutlined },
];

export function ThemeSwitch() {
  const { t } = useTranslation();
  const choice = useThemeStore((state) => state.choice);
  const setChoice = useThemeStore((state) => state.setChoice);

  return (
    <Segmented
      size="small"
      value={choice}
      onChange={(next) => setChoice(next as ThemeChoice)}
      aria-label={t('theme.label')}
      options={CHOICES.map(({ value, labelKey, Icon }) => ({
        value,
        // Icons only: three words in the top bar of a dense console is
        // three words competing with the page title. The name is still
        // reachable, and still what a screen reader announces.
        label: (
          <Tooltip title={t(labelKey)}>
            <span role="img" aria-label={t(labelKey)}>
              <Icon />
            </span>
          </Tooltip>
        ),
      }))}
    />
  );
}
