import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import './i18n';
import '@fontsource-variable/figtree';
import '@fontsource-variable/jetbrains-mono';
import './index.css';
import { initTheme } from './lib/theme';
import App from './App';

initTheme();

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </StrictMode>,
);
