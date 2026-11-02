export type Theme = 'dark' | 'light';

export function currentTheme(): Theme {
  return document.documentElement.dataset.theme === 'light' ? 'light' : 'dark';
}

export function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme;
  localStorage.setItem('theme', theme);
}

/** Reads the stored choice, else follows the device. Dark (OLED) is the fallback. */
export function initTheme() {
  const stored = localStorage.getItem('theme');
  const prefersLight = window.matchMedia?.('(prefers-color-scheme: light)').matches ?? false;
  applyTheme(stored === 'light' || stored === 'dark' ? stored : prefersLight ? 'light' : 'dark');
}
