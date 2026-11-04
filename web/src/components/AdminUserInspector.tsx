import { useEffect, useRef } from 'react';
import { X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { User } from '../lib/api';

export type UserAction = 'status' | 'admin' | 'password' | 'sessions' | 'delete';
const ACTION = 'border-line hover:bg-sunk min-h-11 w-full rounded-row border px-3 py-2 text-left text-[13px] disabled:cursor-default disabled:opacity-50 md:min-h-9';

export default function AdminUserInspector({ user, self, onClose, onAction }: {
  user: User;
  self: boolean;
  onClose: () => void;
  onAction: (action: UserAction) => void;
}) {
  const { t, i18n } = useTranslation();
  const panel = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!window.matchMedia?.('(max-width:720px)').matches) return;
    const opener = document.activeElement;
    const stops = [...panel.current!.querySelectorAll<HTMLButtonElement>('button:not([disabled])')];
    stops[0]?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (document.querySelector('[role="dialog"][aria-modal="true"]')) return;
      if (event.key === 'Escape') onClose();
      if (event.key !== 'Tab') return;
      const first = stops[0], last = stops.at(-1);
      if (!panel.current?.contains(document.activeElement) || event.shiftKey && document.activeElement === first) { event.preventDefault(); (event.shiftKey ? last : first)?.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
    };
    window.addEventListener('keydown', onKey);
    return () => { window.removeEventListener('keydown', onKey); if (opener instanceof HTMLElement && opener.isConnected) opener.focus(); };
  }, [onClose]);

  return (
    <aside ref={panel} aria-label={t('adminUsers.details')} className="bg-surface border-line fixed inset-0 z-40 flex min-w-0 flex-col md:static md:z-auto md:w-[340px] md:flex-none md:border-l xl:w-[360px]">
      <header className="border-line flex min-h-16 items-center justify-between gap-2 border-b px-4"><h2 className="text-ink2 text-xs">{t('adminUsers.details')}</h2><button type="button" aria-label={t('adminUsers.closeDetails')} onClick={onClose} className="text-ink2 grid size-11 place-items-center md:size-9"><X className="size-4" aria-hidden /></button></header>
      <div className="min-h-0 flex-1 space-y-6 overflow-auto p-5">
        <div className="grid gap-2"><p className="text-ink2 text-xs">{t('adminUsers.instanceAccount')}</p><h3 className="text-xl font-bold [overflow-wrap:anywhere]">{user.username}{self && <span className="text-ink2 ml-2 text-xs font-normal">{t('adminUsers.you')}</span>}</h3><p className="text-ink2 text-[13px] [overflow-wrap:anywhere]">{user.email}</p><p className="text-ink2 flex flex-wrap gap-3 text-xs"><span>{t(`adminUsers.${user.status === 'active' ? 'active' : 'disabled'}`)}</span><span>{t(`adminUsers.${user.is_instance_admin ? 'admin' : 'regularUser'}`)}</span></p></div>
        <dl className="grid grid-cols-[88px_minmax(0,1fr)] gap-3 text-xs"><dt className="text-ink2">{t('adminUsers.created')}</dt><dd className="min-w-0">{new Date(user.created_at).toLocaleDateString(i18n.language)}</dd><dt className="text-ink2">{t('adminUsers.email')}</dt><dd>{t(`adminUsers.${user.email_verified ? 'verified' : 'unverified'}`)}</dd><dt className="text-ink2">ID</dt><dd className="min-w-0 font-mono [overflow-wrap:anywhere]">{user.id}</dd></dl>
        <section className="grid gap-2"><h4 className="text-ink2 mb-1 text-xs font-semibold">{t('adminUsers.access')}</h4><button type="button" disabled={self} onClick={() => onAction('status')} className={ACTION}>{t(`adminUsers.${user.status === 'active' ? 'disable' : 'activate'}`)}</button><button type="button" disabled={self} onClick={() => onAction('admin')} className={ACTION}>{t(`adminUsers.${user.is_instance_admin ? 'demote' : 'promote'}`)}</button><button type="button" disabled={self} onClick={() => onAction('password')} className={ACTION}>{t('adminUsers.resetPassword')}</button>{self && <p className="text-ink2 py-1 text-xs leading-relaxed">{t('adminUsers.selfHelp')}</p>}</section>
        <section className="grid gap-2"><h4 className="text-ink2 mb-1 text-xs font-semibold">{t('adminUsers.sessions')}</h4><button type="button" onClick={() => onAction('sessions')} className={ACTION}>{t('adminUsers.viewSessions')}</button></section>
        <section className="grid gap-2"><h4 className="text-ink2 mb-1 text-xs font-semibold">{t('adminUsers.deletion')}</h4><button type="button" disabled={self} onClick={() => onAction('delete')} className={`${ACTION} text-err`}>{t('adminUsers.delete')}</button><p className="text-ink2 text-xs leading-relaxed">{t('adminUsers.deleteHelp')}</p></section>
      </div>
    </aside>
  );
}
