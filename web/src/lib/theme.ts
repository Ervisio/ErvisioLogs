export type Theme = 'dark' | 'light' | 'amoled';

export function currentTheme(): Theme {
  const theme = document.documentElement.dataset.theme;
  return theme === 'light' || theme === 'amoled' ? theme : 'dark';
}

export function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme;
  localStorage.setItem('theme', theme);
}

/** Reads the stored choice, else follows the device. Dark is the fallback. */
export function initTheme() {
  const stored = localStorage.getItem('theme');
  const prefersLight = window.matchMedia?.('(prefers-color-scheme: light)').matches ?? false;
  applyTheme(stored === 'light' || stored === 'dark' || stored === 'amoled' ? stored : prefersLight ? 'light' : 'dark');
}
