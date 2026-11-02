import { useState } from 'react';
import { NavLink, Outlet } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Activity, ChevronsLeft, ChevronsRight, LayoutGrid, Moon, Search, Settings, Sun } from 'lucide-react';
import { cn } from '../lib/cn';
import { applyTheme, currentTheme } from '../lib/theme';
import { languages, type Language } from '../i18n';

type Item = { to: string; icon: typeof LayoutGrid; label: string; ready: boolean };

export default function Shell() {
  const { t, i18n } = useTranslation();
  const [wide, setWide] = useState(() => localStorage.getItem('rail') === 'wide');
  const [theme, setTheme] = useState(currentTheme);

  const items: Item[] = [
    { to: '/', icon: LayoutGrid, label: t('nav.overview'), ready: true },
    { to: '/explorer', icon: Search, label: t('nav.explorer'), ready: false },
    { to: '/tail', icon: Activity, label: t('nav.tail'), ready: false },
  ];

  const toggleRail = () => {
    setWide((w) => {
      localStorage.setItem('rail', w ? 'narrow' : 'wide');
      return !w;
    });
  };
  const toggleTheme = () => {
    const next = theme === 'dark' ? 'light' : 'dark';
    applyTheme(next);
    setTheme(next);
  };

  return (
    <div className="flex min-h-screen gap-2.5 p-2.5 pb-[84px] md:pb-2.5">
      <nav
        aria-label="Primary"
        className={cn(
          'bg-surface fixed inset-x-2 bottom-2 z-10 flex h-16 items-center justify-around rounded-card p-1.5',
          'md:static md:h-auto md:flex-col md:justify-start md:gap-2 md:px-2 md:py-3.5 md:transition-[width] md:duration-200',
          wide ? 'md:w-[200px] md:items-stretch' : 'md:w-[88px] md:items-center',
        )}
      >
        <div className="bg-hue mx-auto mb-1.5 hidden size-11 rounded-[14px] md:block" aria-hidden />
        {items.map(({ to, icon: Icon, label, ready }) => (
          <NavLink
            key={to}
            to={ready ? to : '#'}
            end
            aria-disabled={!ready}
            title={ready ? label : `${label}: ${t('nav.soon')}`}
            onClick={(e) => !ready && e.preventDefault()}
            className={({ isActive }) =>
              cn(
                'text-ink2 flex flex-1 flex-col items-center gap-1 rounded-[14px] px-1 py-2 text-xs font-semibold md:flex-none',
                wide && 'md:flex-row md:gap-3 md:px-3.5 md:py-2.5 md:text-sm',
                !ready && 'opacity-60',
                ready && isActive && 'text-hue bg-[color-mix(in_srgb,var(--hue)_16%,var(--surface))]',
              )
            }
          >
            <Icon className="size-6" aria-hidden />
            <span>{label}</span>
          </NavLink>
        ))}
        <div className="hidden flex-1 md:block" />
        <button
          type="button"
          aria-disabled
          title={`${t('nav.settings')}: ${t('nav.soon')}`}
          className={cn(
            'text-ink2 hidden flex-col items-center gap-1 rounded-[14px] px-1 py-2 text-xs font-semibold opacity-60 md:flex',
            wide && 'md:flex-row md:gap-3 md:px-3.5 md:py-2.5 md:text-sm',
          )}
        >
          <Settings className="size-6" aria-hidden />
          <span>{t('nav.settings')}</span>
        </button>
        <button
          type="button"
          onClick={toggleRail}
          aria-expanded={wide}
          aria-label={wide ? t('nav.collapse') : t('nav.expand')}
          className={cn(
            'text-ink2 hover:bg-sunk hidden items-center gap-3 rounded-[14px] px-1 py-2 text-xs font-semibold md:flex md:flex-col',
            wide && 'md:flex-row md:px-3.5 md:py-2.5 md:text-sm',
          )}
        >
          {wide ? <ChevronsLeft className="size-6" aria-hidden /> : <ChevronsRight className="size-6" aria-hidden />}
          <span>{wide ? t('nav.collapse') : t('nav.expand')}</span>
        </button>
      </nav>

      <div className="flex min-w-0 flex-1 flex-col gap-2.5">
        <header className="flex gap-2.5">
          <div className="bg-surface text-ink3 flex h-10 flex-1 items-center gap-2.5 rounded-control px-3.5">
            <Search className="size-4" aria-hidden />
            <span>{t('search.placeholder')}</span>
          </div>
          <label className="bg-surface text-ink2 flex h-10 items-center gap-2 rounded-control px-3">
            <span className="sr-only">{t('language')}</span>
            <select
              value={i18n.language}
              onChange={(e) => void i18n.changeLanguage(e.target.value as Language)}
              className="bg-transparent font-semibold uppercase"
            >
              {languages.map((l) => (
                <option key={l} value={l} className="bg-surface text-ink">
                  {l}
                </option>
              ))}
            </select>
          </label>
          <button
            type="button"
            onClick={toggleTheme}
            aria-label={t('theme.toggle')}
            className="bg-surface text-ink2 hover:text-ink flex h-10 w-10 items-center justify-center rounded-control"
          >
            {theme === 'dark' ? <Sun className="size-[18px]" aria-hidden /> : <Moon className="size-[18px]" aria-hidden />}
          </button>
        </header>
        <main className="bg-surface flex-1 rounded-card p-5 md:p-8">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
