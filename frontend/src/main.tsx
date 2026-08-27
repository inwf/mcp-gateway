import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import '@/i18n';
import '@/styles/tokens.css';
import '@/styles/global.css';
import { App } from '@/App';

const container = document.getElementById('root');
if (!container) throw new Error('the page has no #root to mount into');

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
