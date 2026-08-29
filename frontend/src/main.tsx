import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import '@/i18n';
import '@/styles/tokens.css';
import '@/styles/global.css';
import { App } from '@/App';
import { applyMode, resolveMode, storedChoice } from '@/stores/theme';

// Before anything renders. The first paint has to already be in the
// chosen theme: doing this from inside React would show the default for
// one frame and then correct itself, which is a visible flash on every
// load for anyone whose choice is not the default.
applyMode(resolveMode(storedChoice()));

const container = document.getElementById('root');
if (!container) throw new Error('the page has no #root to mount into');

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
