import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Activity, LogOut, Moon, Sun } from 'lucide-react';
import { useAuth, useSession } from '../lib/auth';
import { useTenancy } from '../lib/tenancy';
import { applyTheme, currentTheme } from '../lib/theme';
import { languages, type Language } from '../i18n';

const ROW = 'text-ink2 hover:bg-sunk hover:text-ink flex min-h-11 w-full items-center gap-2 rounded-row px-2 text-left md:min-h-9';
const SELECT = 'bg-sunk text-ink border-field h-11 max-w-[150px] truncate rounded-row border px-1.5 md:h-8';

export default function AccountMenu({ onClose, onNavigate }: { onClose: () => void; onNavigate: () => void }) {
  const { t, i18n } = useTranslation();
  const { user } = useSession();
  const { logout } = useAuth();
  const { workspaces, workspace, selectWorkspace } = useTenancy();
  const [theme, setTheme] = useState(currentTheme);
  const [failed, setFailed] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    ref.current?.querySelector<HTMLElement>('button, a')?.focus();
    const onDown = (e: MouseEvent) => {
      const target = e.target as Element;
      if (!ref.current?.contains(target) && !target.closest('[data-account-button]')) onClose();
    };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [onClose]);

  const toggleTheme = () => {
    const next = theme === 'dark' ? 'light' : 'dark';
    applyTheme(next);
    setTheme(next);
  };

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.stopPropagation();
      onClose();
      document.querySelector<HTMLElement>('[data-account-button]')?.focus();
      return;
    }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    if ((e.target as Element).tagName === 'SELECT') return; // arrows change the select's value
    const rows = Array.from(ref.current?.querySelectorAll<HTMLElement>('button, a') ?? []);
    const at = rows.indexOf(document.activeElement as HTMLElement);
    e.preventDefault();
    rows[(at + (e.key === 'ArrowDown' ? 1 : -1) + rows.length) % rows.length]?.focus();
  };

  return (
    <div
      ref={ref}
      role="group"
      aria-label={t('account.open')}
      onKeyDown={onKeyDown}
      className="bg-surface border-raised fixed inset-x-0 bottom-0 z-55 max-h-[82dvh] overflow-auto rounded-t-[10px] border-t p-2 pb-[calc(0.5rem+env(safe-area-inset-bottom))] md:inset-x-auto md:bottom-3 md:left-[var(--rail-now)] md:ml-2 md:w-64 md:rounded-[10px] md:border md:pb-2"
    >
      <div className="px-2 pt-1 pb-2">
        <p className="truncate font-semibold">{user.username}</p>
        <p className="text-ink2 truncate text-[13px]">{user.email}</p>
      </div>
      <div className="bg-line -mx-2 my-1 h-px" />
      {workspaces.length > 1 && (
        <label className="text-ink2 flex items-center justify-between gap-2 px-2 py-1 text-[13px]">
          {t('account.workspace')}
          <select value={workspace?.id} onChange={(e) => selectWorkspace(e.target.value)} className={SELECT}>
            {workspaces.map((w) => (
              <option key={w.id} value={w.id}>
                {w.name}
              </option>
            ))}
          </select>
        </label>
      )}
      <label className="text-ink2 flex items-center justify-between gap-2 px-2 py-1 text-[13px]">
        {t('language')}
        <select value={i18n.resolvedLanguage} onChange={(e) => void i18n.changeLanguage(e.target.value as Language)} className={SELECT}>
          {languages.map((l) => (
            <option key={l} value={l}>
              {l === 'en' ? 'English' : 'Italiano'}
            </option>
          ))}
        </select>
      </label>
      <button type="button" onClick={toggleTheme} aria-label={t('theme.toggle')} className={ROW}>
        {theme === 'dark' ? <Sun className="size-4" aria-hidden /> : <Moon className="size-4" aria-hidden />}
        {theme === 'dark' ? t('theme.light') : t('theme.dark')}
      </button>
      <Link to="/status" onClick={onNavigate} className={ROW}>
        <Activity className="size-4" aria-hidden />
        {t('nav.status')}
      </Link>
      <div className="bg-line -mx-2 my-1 h-px" />
      <button
        type="button"
        onClick={() => {
          setFailed(false);
          logout().catch(() => setFailed(true));
        }}
        className={ROW}
      >
        <LogOut className="size-4" aria-hidden />
        {t('account.signOut')}
      </button>
      {failed && (
        <p role="alert" className="text-err px-2 py-1 text-[13px]">
          {t('account.signOutFailed')}
        </p>
      )}
    </div>
  );
}
