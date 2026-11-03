import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Search, type LucideIcon } from 'lucide-react';
import { cn } from '../lib/cn';

export type Command = { id: string; group: string; label: string; icon: LucideIcon; run: () => void };

export default function CommandPalette({ commands, onClose }: { commands: Command[]; onClose: () => void }) {
  const { t } = useTranslation();
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const returnTo = useRef<Element | null>(document.activeElement);

  useEffect(() => {
    inputRef.current?.focus();
    const previous = returnTo.current;
    return () => {
      if (previous instanceof HTMLElement) previous.focus();
    };
  }, []);

  const matches = useMemo(() => {
    const q = query.trim().toLowerCase();
    return q ? commands.filter((c) => c.label.toLowerCase().includes(q)) : commands;
  }, [commands, query]);
  const current = Math.min(active, Math.max(matches.length - 1, 0));

  const choose = (command?: Command) => {
    if (!command) return;
    onClose();
    command.run();
  };
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      onClose();
    } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (matches.length) setActive((current + (e.key === 'ArrowDown' ? 1 : -1) + matches.length) % matches.length);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      choose(matches[current]);
    } else if (e.key === 'Tab') {
      e.preventDefault(); // the input is the only stop inside this dialog
    }
  };

  return (
    <div className="fixed inset-0 z-60 flex items-start justify-center bg-black/60 p-3 pt-[12vh]" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div role="dialog" aria-modal="true" aria-label={t('palette.title')} onKeyDown={onKeyDown} className="bg-surface border-raised flex max-h-[70dvh] w-full max-w-[560px] flex-col overflow-hidden rounded-[10px] border">
        <div className="border-line focus-within:border-hue flex items-center gap-2 border-b px-3">
          <Search className="text-ink3 size-4 flex-none" aria-hidden />
          <input
            ref={inputRef}
            role="combobox"
            aria-expanded="true"
            aria-controls="palette-list"
            aria-activedescendant={matches[current] ? `palette-${matches[current].id}` : undefined}
            aria-label={t('palette.placeholder')}
            placeholder={t('palette.placeholder')}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setActive(0);
            }}
            className="placeholder:text-ink3 h-12 min-w-0 flex-1 bg-transparent text-base outline-none md:text-sm"
          />
        </div>
        <ul id="palette-list" role="listbox" aria-label={t('palette.title')} className="min-h-0 flex-1 overflow-y-auto p-2">
          {matches.length === 0 && <li className="text-ink2 px-3 py-4">{t('palette.empty')}</li>}
          {matches.map((c, i) => {
            const heading = i === 0 || matches[i - 1].group !== c.group ? c.group : null;
            const Icon = c.icon;
            return (
              <li key={c.id} role="presentation">
                {heading && <p className="text-ink3 px-3 pt-2 pb-1 text-[12.5px] font-semibold">{heading}</p>}
                <div
                  id={`palette-${c.id}`}
                  role="option"
                  aria-selected={i === current}
                  onMouseEnter={() => setActive(i)}
                  onClick={() => choose(c)}
                  className={cn('flex min-h-11 cursor-pointer items-center gap-3 rounded-row px-3 md:min-h-[38px]', i === current && 'bg-raised')}
                >
                  <Icon className={cn('size-4 flex-none', i === current ? 'text-hue' : 'text-ink2')} aria-hidden />
                  <span className="min-w-0 flex-1 truncate">{c.label}</span>
                </div>
              </li>
            );
          })}
        </ul>
        <p className="border-line text-ink2 hidden gap-4 border-t px-4 py-2 text-[12.5px] md:flex">
          <span>{t('palette.hintMove')}</span>
          <span>{t('palette.hintOpen')}</span>
          <span>{t('palette.hintClose')}</span>
        </p>
      </div>
    </div>
  );
}
