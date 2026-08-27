import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import { zh } from './zh';

/*
 * Only Chinese is filled in. The framework is here from the start so
 * that adding a language later is a new file rather than a rewrite of
 * every component.
 */

void i18n.use(initReactI18next).init({
  resources: { zh: { translation: zh } },
  lng: 'zh',
  fallbackLng: 'zh',

  interpolation: {
    // React escapes what it renders already; doing it again turns
    // a server name containing & into an entity on screen.
    escapeValue: false,
  },

  // A key with no entry shows the key itself rather than an empty
  // space. An empty space in a button is invisible in review; the key
  // is not, which is the point.
  parseMissingKeyHandler: (key) => `⟦${key}⟧`,

  returnNull: false,
});

export default i18n;
