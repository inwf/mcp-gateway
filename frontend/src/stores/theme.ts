import { create } from 'zustand';
import { persist } from 'zustand/middleware';

/**
 * The theme, and what "follow the system" means.
 *
 * Three choices, two outcomes. Light and dark are answers; "system" is a
 * deferral, and it has to be resolved before anything can be painted.
 * That resolution happens here, once, and everything downstream — the
 * stylesheets, antd — only ever sees light or dark. The alternative would
 * be a prefers-color-scheme query in every stylesheet, which cannot
 * express "the user asked for dark on a light system".
 *
 * The choice is kept in the browser rather than in the gateway's
 * configuration. It belongs to whoever is looking at the screen, not to
 * the installation: two people using the same gateway from two machines
 * should not be arguing over one setting.
 */

export type ThemeChoice = 'light' | 'dark' | 'system';
export type ThemeMode = 'light' | 'dark';

/** The key the choice is stored under. Named for the project so that it
 *  cannot collide with anything else served from the same origin. */
const STORAGE_KEY = 'mcphub.theme';

const DARK_QUERY = '(prefers-color-scheme: dark)';

/** What the system currently prefers, defaulting to dark where the
 *  question cannot be asked — that is this application's own default, so
 *  an environment without matchMedia gets the same answer as one whose
 *  user has expressed no preference. */
export function systemMode(): ThemeMode {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return 'dark';
  return window.matchMedia(DARK_QUERY).matches ? 'dark' : 'light';
}

export function resolveMode(choice: ThemeChoice): ThemeMode {
  return choice === 'system' ? systemMode() : choice;
}

interface ThemeState {
  choice: ThemeChoice;
  /** The resolved answer. Kept in the store rather than derived at each
   *  read so that a change of system preference re-renders what depends
   *  on it. */
  mode: ThemeMode;
  setChoice: (choice: ThemeChoice) => void;
  /** Re-resolves without changing the choice, for when the system's
   *  preference changes underneath a "system" choice. */
  resync: () => void;
}

export const useThemeStore = create<ThemeState>()(
  persist(
    (set, get) => ({
      choice: 'system',
      mode: systemMode(),

      setChoice: (choice) => set({ choice, mode: resolveMode(choice) }),
      resync: () => set({ mode: resolveMode(get().choice) }),
    }),
    {
      name: STORAGE_KEY,
      // Only the choice is stored. Storing the resolved mode as well
      // would mean a stale answer surviving a reboot into a differently
      // configured system.
      partialize: (state) => ({ choice: state.choice }),
      onRehydrateStorage: () => (state) => state?.resync(),
    },
  ),
);

/**
 * Writes the resolved theme onto the document.
 *
 * The attribute is what the stylesheets switch on, and color-scheme is
 * what the browser's own furniture — scrollbars, form controls, the
 * canvas behind an overscroll — reads. Setting only the first leaves a
 * light page with dark scrollbars.
 */
export function applyMode(mode: ThemeMode): void {
  if (typeof document === 'undefined') return;
  document.documentElement.dataset['theme'] = mode;
  document.documentElement.style.colorScheme = mode;
}

/**
 * Reads the stored choice without starting the store.
 *
 * This exists for the script that runs before the application does: the
 * first paint has to already be in the right theme, and a store created
 * during React's first render is too late — the page would flash the
 * default and then correct itself.
 */
export function storedChoice(): ThemeChoice {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return 'system';
    const parsed = JSON.parse(raw) as { state?: { choice?: unknown } };
    const choice = parsed.state?.choice;
    return choice === 'light' || choice === 'dark' ? choice : 'system';
  } catch {
    // A quota error, a disabled store, or something else's data under our
    // key. None of them is worth failing a page load over.
    return 'system';
  }
}
