import type { AttrFilter } from './api';

export type ParsedQuery = { text: string; filters: AttrFilter[] };

// key=value, key!=value, key~value (contains), key>=n, key<=n, key>n, key<n, has:key.
// Keys follow the ingest alphabet; values may be quoted to hold spaces.
const TERM = /^([A-Za-z0-9_.-]+)(!=|>=|<=|=|~|>|<)(.*)$/;
const OPS: Record<string, string> = { '=': 'eq', '!=': 'neq', '~': 'contains', '>': 'gt', '>=': 'gte', '<': 'lt', '<=': 'lte' };

function tokens(input: string): string[] {
  const out: string[] = [];
  let cur = '';
  let quoted = false;
  for (const ch of input) {
    if (ch === '"') quoted = !quoted;
    else if (/\s/.test(ch) && !quoted) {
      if (cur) out.push(cur);
      cur = '';
    } else cur += ch;
  }
  if (cur) out.push(cur);
  return out;
}

/** Splits the search box into attribute filters and the free text matched against messages. */
export function parseQuery(input: string): ParsedQuery {
  const text: string[] = [];
  const filters: AttrFilter[] = [];
  for (const tok of tokens(input)) {
    if (tok.startsWith('has:') && tok.length > 4) {
      filters.push({ field: tok.slice(4), op: 'exists' });
      continue;
    }
    const m = TERM.exec(tok);
    if (m && m[3] !== '') filters.push({ field: m[1], op: OPS[m[2]], value: m[3] });
    else text.push(tok);
  }
  return { text: text.join(' '), filters };
}

const SYMBOL: Record<string, string> = Object.fromEntries(Object.entries(OPS).map(([sym, op]) => [op, sym]));
const quote = (v: string) => (/[\s"]/.test(v) ? `"${v.replace(/"/g, '')}"` : v);

/** The inverse of parseQuery: the search box text for a saved filter. */
export function formatQuery(text: string | undefined, filters: AttrFilter[] | undefined): string {
  const parts = (filters ?? []).map((f) => (f.op === 'exists' ? `has:${f.field}` : `${f.field}${SYMBOL[f.op] ?? '='}${quote(f.value ?? '')}`));
  if (text) parts.push(text);
  return parts.join(' ');
}
