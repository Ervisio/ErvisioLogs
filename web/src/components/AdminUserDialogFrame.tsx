import { useEffect, useRef, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { X } from 'lucide-react';

export default function AdminUserDialogFrame({ title, children, onClose, busy = false }: { title: string; children: ReactNode; onClose: () => void; busy?: boolean }) {
  const { t } = useTranslation();
  const panel = useRef<HTMLElement>(null);
  const close = useRef<HTMLButtonElement>(null);
  const busyRef = useRef(busy);
  useEffect(() => { busyRef.current = busy; }, [busy]);
  useEffect(() => {
    const opener = document.activeElement;
    close.current?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !busyRef.current) onClose();
      if (event.key !== 'Tab') return;
      const stops = [...panel.current!.querySelectorAll<HTMLElement>('button:not([disabled]),input:not([disabled]),select:not([disabled])')];
      const first = stops[0], last = stops.at(-1);
      if (!panel.current?.contains(document.activeElement) || event.shiftKey && document.activeElement === first) { event.preventDefault(); (event.shiftKey ? last : first)?.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
    };
    window.addEventListener('keydown', onKey);
    return () => { window.removeEventListener('keydown', onKey); if (opener instanceof HTMLElement && opener.isConnected) opener.focus(); };
  }, [onClose]);
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/50 p-2 md:p-5"><section ref={panel} role="dialog" aria-modal="true" aria-labelledby="admin-user-dialog-title" aria-busy={busy} className="bg-surface border-line flex max-h-[96dvh] w-full max-w-[560px] flex-col overflow-hidden rounded-row border md:max-h-[90dvh]"><header className="border-line flex flex-none items-center gap-3 border-b px-4 py-3"><h2 id="admin-user-dialog-title" className="min-w-0 flex-1 text-base font-bold [overflow-wrap:anywhere]">{title}</h2><button ref={close} type="button" disabled={busy} aria-label={t('adminUsers.closeDialog')} onClick={onClose} className="text-ink2 grid size-11 flex-none place-items-center disabled:opacity-50 md:size-9"><X className="size-4" aria-hidden /></button></header><div className="min-h-0 space-y-4 overflow-auto p-4">{children}</div></section></div>
  );
}
