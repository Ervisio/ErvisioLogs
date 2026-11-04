import { useState } from 'react';
import { Link, NavLink, useOutletContext, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ArrowLeft, BookOpen, Check, Copy, Search } from 'lucide-react';
import type { ShellContext } from '../components/Shell';
import { cn } from '../lib/cn';

type Article = {
  id: string;
  title: string;
  summary: string;
  sections: { title: string; paragraphs: string[]; code?: string; language?: string; steps?: string[] }[];
};

function CodeExample({ code, language }: { code: string; language?: string }) {
  const { t } = useTranslation();
  const [status, setStatus] = useState<'idle' | 'copied' | 'failed'>('idle');
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      setStatus('copied');
    } catch {
      setStatus('failed');
    }
  };
  return (
    <div className="bg-sunk border-line min-w-0 overflow-hidden rounded-row border">
      <div className="border-line flex items-center justify-between gap-3 border-b px-3 py-1">
        <span className="text-ink2 font-mono text-xs">{language}</span>
        <button type="button" onClick={() => void copy()} className="text-ink2 hover:text-ink flex min-h-11 items-center gap-2 text-[13px] font-semibold">
          {status === 'copied' ? <Check className="size-4" aria-hidden /> : <Copy className="size-4" aria-hidden />}
          {t(status === 'copied' ? 'detail.copied' : 'docs.copy')}
        </button>
      </div>
      <pre className="overflow-x-auto p-4 font-mono text-[12px] leading-relaxed" tabIndex={0}><code>{code}</code></pre>
      <p role="status" className={status === 'failed' ? 'text-ink2 px-4 pb-3 text-[13px]' : 'sr-only'}>
        {status === 'failed' ? t('docs.copyFailed') : status === 'copied' ? t('detail.copied') : ''}
      </p>
    </div>
  );
}

/** Bundled help: usable without a project, an admin role, or an external documentation service. */
export default function Documentation() {
  const { t } = useTranslation();
  const { topbar } = useOutletContext<ShellContext>();
  const { topic } = useParams();
  const [query, setQuery] = useState('');
  const articles = t('docs.articles', { returnObjects: true }) as Article[];
  const needle = query.trim().toLocaleLowerCase();
  const matches = articles.filter((article) =>
    [article.title, article.summary, ...article.sections.flatMap((s) => [s.title, ...s.paragraphs, ...(s.steps ?? []), s.code ?? ''])]
      .join(' ').toLocaleLowerCase().includes(needle),
  );
  const article = articles.find((item) => item.id === topic);

  return (
    <>
      {topbar(<h1 className="truncate text-base font-bold">{t('nav.documentation')}</h1>)}
      <div className="flex-1 overflow-auto p-4 md:p-6">
        <div className="mx-auto grid max-w-[1100px] grid-cols-[minmax(0,1fr)] gap-6 lg:grid-cols-[240px_minmax(0,1fr)] lg:gap-10">
          <aside className="min-w-0">
            <label htmlFor="docs-search" className="mb-2 block text-[13px] font-semibold">{t('docs.search')}</label>
            <div className="border-field bg-sunk flex items-center gap-2 rounded-row border px-3">
              <Search className="text-ink3 size-4 flex-none" aria-hidden />
              <input id="docs-search" type="search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t('docs.searchPlaceholder')} className="min-h-11 min-w-0 flex-1 bg-transparent outline-none" />
            </div>
            <nav aria-label={t('docs.topics')} className="mt-3 flex max-h-44 flex-wrap gap-1 overflow-auto lg:grid lg:max-h-none lg:overflow-visible">
              <NavLink to="/docs" end className={({ isActive }) => cn('flex min-h-11 items-center gap-2 rounded-row px-3 font-semibold', isActive ? 'bg-raised text-ink' : 'text-ink2 hover:bg-sunk')}>
                <BookOpen className="size-4" aria-hidden />{t('docs.allTopics')}
              </NavLink>
              {matches.map((item) => (
                <NavLink key={item.id} to={`/docs/${item.id}`} className={({ isActive }) => cn('flex min-h-11 items-center rounded-row px-3 text-[13px]', isActive ? 'bg-raised text-ink font-semibold' : 'text-ink2 hover:bg-sunk')}>
                  {item.title}
                </NavLink>
              ))}
            </nav>
            {needle && <p role="status" className="text-ink2 mt-3 px-3 text-[13px]">{t('docs.results', { count: matches.length })}</p>}
            {needle && matches.length === 0 && (
              <button type="button" onClick={() => setQuery('')} className="text-hue min-h-11 px-3 font-semibold underline underline-offset-2">{t('docs.clearSearch')}</button>
            )}
          </aside>

          <div className="min-w-0">
            {topic ? article ? (
              <article key={article.id} aria-labelledby="doc-title" className="grid min-w-0 gap-7">
                <div>
                  <Link to="/docs" className="text-hue mb-4 inline-flex min-h-11 items-center gap-2 text-[13px] font-semibold"><ArrowLeft className="size-4" aria-hidden />{t('docs.allTopics')}</Link>
                  <h2 id="doc-title" className="mb-2 text-2xl font-bold">{article.title}</h2>
                  <p className="text-ink2 max-w-[70ch] leading-relaxed">{article.summary}</p>
                </div>
                {article.sections.map((section, index) => (
                  <section key={index} aria-labelledby={`doc-section-${index}`} className="grid min-w-0 gap-3">
                    <h3 id={`doc-section-${index}`} className="text-[17px] font-bold">{section.title}</h3>
                    {section.paragraphs.map((paragraph) => <p key={paragraph} className="text-ink2 max-w-[70ch] leading-relaxed">{paragraph}</p>)}
                    {section.steps && <ol className="text-ink2 grid list-decimal gap-3 pl-5 leading-relaxed">{section.steps.map((step) => <li key={step}>{step}</li>)}</ol>}
                    {section.code && <CodeExample code={section.code} language={section.language} />}
                  </section>
                ))}
              </article>
            ) : (
              <div className="py-8">
                <h2 className="mb-2 text-xl font-bold">{t('docs.notFound')}</h2>
                <Link to="/docs" className="text-hue inline-flex min-h-11 items-center font-semibold underline underline-offset-2">{t('docs.allTopics')}</Link>
              </div>
            ) : (
              <>
                <h2 className="mb-2 text-2xl font-bold">{t('docs.title')}</h2>
                <p className="text-ink2 mb-6 max-w-[70ch] leading-relaxed">{t('docs.intro')}</p>
                {matches.length === 0 ? <p role="status" className="text-ink2">{t('docs.noResults')}</p> : (
                  <div className="grid gap-3 sm:grid-cols-2">
                    {matches.map((item) => (
                      <Link key={item.id} to={`/docs/${item.id}`} className="border-line hover:bg-sunk min-w-0 rounded-row border p-4">
                        <h3 className="mb-2 font-bold">{item.title}</h3>
                        <p className="text-ink2 text-[13px] leading-relaxed">{item.summary}</p>
                      </Link>
                    ))}
                  </div>
                )}
              </>
            )}
          </div>
        </div>
      </div>
    </>
  );
}
