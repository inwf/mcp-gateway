import { useEffect } from 'react';
import { applyMode, useThemeStore } from '@/stores/theme';

/**
 * Keeps the document's theme in step with the chosen one, and follows the
 * system while that is what was chosen.
 *
 * Returns the resolved mode, which is what antd needs: it takes a whole
 * configuration rather than reading the document.
 */
export function useTheme() {
  const mode = useThemeStore((state) => state.mode);
  const choice = useThemeStore((state) => state.choice);
  const resync = useThemeStore((state) => state.resync);

  useEffect(() => applyMode(mode), [mode]);

  useEffect(() => {
    // Only worth listening while the choice defers to the system. An
    // explicit light or dark does not change when the system does.
    if (choice !== 'system') return;
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;

    const query = window.matchMedia('(prefers-color-scheme: dark)');
    const onChange = () => resync();
    query.addEventListener('change', onChange);
    return () => query.removeEventListener('change', onChange);
  }, [choice, resync]);

  return mode;
}
