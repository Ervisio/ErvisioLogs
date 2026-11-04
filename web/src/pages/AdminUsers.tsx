import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { useOutletContext } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import type { ShellContext } from '../components/Shell';
import { ApiError, type User } from '../lib/api';
import { adminUsers } from '../lib/adminUsers';
import { useAuth, useSession } from '../lib/auth';
import { cn } from '../lib/cn';
import AdminUserInspector, { type UserAction } from '../components/AdminUserInspector';
import AdminUserActions, { type AdminUserModal } from '../components/AdminUserActions';

type UserPage = { items: User[]; next_cursor: string | null };
type Loaded = { key: string; page: UserPage } | { key: string; error: string };

export default function AdminUsers() {
  const { t } = useTranslation();
  const { user } = useSession();
  const { retry } = useAuth();
  const { topbar } = useOutletContext<ShellContext>();
  const [input, setInput] = useState('');
  const [query, setQuery] = useState('');
  const [cursors, setCursors] = useState(['']);
  const [pageIndex, setPageIndex] = useState(0);
  const [attempt, setAttempt] = useState(0);
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const [selected, setSelected] = useState<User | null>(null);
  const [modal, setModal] = useState<AdminUserModal | null>(null);
  const [notice, setNotice] = useState('');
  const cursor = cursors[pageIndex];
  const key = JSON.stringify([query, cursor, attempt]);
  const listRef = useRef<HTMLDivElement>(null);
  const closeDetails = useCallback(() => setSelected(null), []);
  const closeModal = useCallback(() => setModal(null), []);

  useEffect(() => {
    if (!user.is_instance_admin) return;
    const ctrl = new AbortController();
    adminUsers.list(query, cursor || undefined, ctrl.signal).then(page => {
      if (!ctrl.signal.aborted) setLoaded({ key, page });
    }).catch(err => {
      if (ctrl.signal.aborted) return;
      if (err instanceof ApiError && err.status === 401) retry();
      setLoaded({ key, error: err instanceof ApiError && err.status < 500 ? err.message : t('adminUsers.loadFailed') });
    });
    return () => ctrl.abort();
  }, [key, query, cursor, retry, t, user.is_instance_admin]);

  const current = loaded?.key === key ? loaded : null;
  const page = current && 'page' in current ? current.page : null;
  const search = (event: FormEvent) => {
    event.preventDefault();
    setQuery(input.trim()); setCursors(['']); setPageIndex(0); setSelected(null); setNotice('');
  };
  const select = (item: User) => { setSelected(item); setModal(null); };
  const openAction = (action: UserAction) => { if (selected) setModal({ action, user: selected }); };
  const changed = (updated: User | null, message: string) => {
    const action = modal?.action;
    setModal(null); setNotice(message); setSelected(updated);
    if (action === 'create' || action === 'delete') setAttempt(value => value + 1);
    else if (updated) setLoaded(previous => previous && 'page' in previous ? { ...previous, page: { ...previous.page, items: previous.page.items.map(item => item.id === updated.id ? updated : item) } } : previous);
  };
  const next = () => {
    if (!page?.next_cursor) return;
    setCursors(previous => [...previous.slice(0, pageIndex + 1), page.next_cursor!]);
    setPageIndex(value => value + 1);
    listRef.current?.scrollTo?.({ top: 0 });
  };

  if (!user.is_instance_admin) return <>{topbar(<h1 className="text-base font-bold">{t('adminUsers.title')}</h1>)}<p role="alert" className="p-6">{t('adminUsers.forbidden')}</p></>;
  return <>
    {topbar(<div className="min-w-0"><p className="text-ink2 text-xs">{t('adminUsers.instanceScope')}</p><h1 className="truncate text-base font-bold">{t('adminUsers.title')}</h1></div>)}
    <section className="border-line flex flex-none flex-wrap items-start justify-between gap-4 border-b p-4 md:px-6"><p className="text-ink2 max-w-[66ch] text-[13px] leading-relaxed">{t('adminUsers.intro')}</p><button type="button" onClick={() => setModal({ action: 'create' })} className="bg-accent text-accent-ink min-h-11 rounded-row px-4 font-semibold md:min-h-9">{t('adminUsers.create')}</button></section>
    <form role="search" onSubmit={search} className="border-line flex flex-none gap-2 border-b p-3 md:px-6"><label className="min-w-0 flex-1"><span className="sr-only">{t('adminUsers.search')}</span><input type="search" value={input} onChange={event => setInput(event.target.value)} maxLength={320} placeholder={t('adminUsers.searchPlaceholder')} className="bg-sunk border-field placeholder:text-ink3 min-h-11 w-full min-w-0 rounded-row border px-3 text-sm md:min-h-9" /></label><button type="submit" className="border-line hover:bg-sunk min-h-11 rounded-row border px-4 text-sm md:min-h-9">{t('adminUsers.search')}</button></form>
    {notice && <p role="status" className="border-line text-ink2 flex-none border-b px-4 py-3 text-[13px] md:px-6">{notice}</p>}
    <div className="flex min-h-0 flex-1 overflow-hidden">
      <section aria-label={t('adminUsers.list')} className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <div ref={listRef} className="min-h-0 flex-1 overflow-auto">
          {!current && <p role="status" className="text-ink2 flex items-center gap-2 p-6"><span className="spinner" aria-hidden />{t('adminUsers.loading')}</p>}
          {current && 'error' in current && <div role="alert" className="space-y-3 p-6"><p className="text-ink2 [overflow-wrap:anywhere]">{current.error}</p><button type="button" onClick={() => setAttempt(value => value + 1)} className="text-hue min-h-11 underline">{t('common.retry')}</button></div>}
          {page?.items.length === 0 && <div className="space-y-2 p-6 text-center"><h2 className="font-semibold">{t('adminUsers.empty')}</h2><p className="text-ink2 text-sm">{t('adminUsers.emptyHelp')}</p></div>}
          {page?.items.map(item => <button type="button" key={item.id} onClick={() => select(item)} aria-label={t('adminUsers.manageUser', { name: item.username })} aria-expanded={selected?.id === item.id} className={cn('border-line hover:bg-sunk flex min-h-24 w-full items-start gap-3 border-b p-4 text-left md:px-6', selected?.id === item.id && 'bg-sunk outline-hue outline-1 -outline-offset-1')}><span aria-hidden className="border-line bg-surface hidden size-9 flex-none place-items-center rounded-row border text-xs font-bold sm:grid">{item.username.slice(0, 2).toUpperCase()}</span><span className="grid min-w-0 flex-1 gap-1"><span className="flex min-w-0 items-center gap-2 font-semibold [overflow-wrap:anywhere]">{item.username}{item.id === user.id && <span className="text-ink2 text-xs font-normal">{t('adminUsers.you')}</span>}</span><span className="text-ink2 text-[13px] [overflow-wrap:anywhere]">{item.email}</span><span className="text-ink2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs"><span className="flex items-center gap-1.5"><span aria-hidden className={cn('size-1.5 rounded-full', item.status === 'active' ? 'bg-ok' : 'bg-field')} />{t(`adminUsers.${item.status === 'active' ? 'active' : 'disabled'}`)}</span><span>{t(`adminUsers.${item.is_instance_admin ? 'admin' : 'regularUser'}`)}</span></span></span></button>)}
        </div>
        <footer className="border-line text-ink2 flex flex-none flex-wrap items-center justify-between gap-2 border-t p-3 text-xs md:px-6"><span>{t('adminUsers.page', { page: pageIndex + 1 })}</span><div className="flex gap-2"><button type="button" disabled={pageIndex === 0 || !page} onClick={() => setPageIndex(value => value - 1)} className="border-line min-h-11 rounded-row border px-3 disabled:opacity-50 md:min-h-9">{t('adminUsers.previous')}</button><button type="button" disabled={!page?.next_cursor} onClick={next} className="border-line min-h-11 rounded-row border px-3 disabled:opacity-50 md:min-h-9">{t('adminUsers.next')}</button></div></footer>
      </section>
      {selected && <AdminUserInspector key={selected.id} user={selected} self={selected.id === user.id} onClose={closeDetails} onAction={openAction} />}
    </div>
    {modal && <AdminUserActions key={`${modal.action}:${'user' in modal ? modal.user.id : ''}`} modal={modal} currentUserId={user.id} onClose={closeModal} onChanged={changed} />}
  </>;
}
