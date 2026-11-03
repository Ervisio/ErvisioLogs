/** Env files edited line by line, so comments and order survive, and random secrets for new installs. */

/** The value of the last active `KEY=` line, or null. */
export function envGet(text: string | null, key: string): string | null {
  if (!text) return null;
  let v: string | null = null;
  for (const line of text.split('\n')) {
    const m = line.replace(/\r$/, '').match(/^(?:export\s+)?([A-Z0-9_]+)=(.*)$/);
    if (m && m[1] === key) v = unquote(m[2]);
  }
  return v;
}

function unquote(v: string): string {
  const s = v.trim();
  if (s.length >= 2 && ((s[0] === '"' && s.endsWith('"')) || (s[0] === "'" && s.endsWith("'")))) return s.slice(1, -1);
  return s;
}

/** Sets KEY=value: replaces the last active line, else uncomments "# KEY=", else appends. */
export function envSet(text: string, key: string, value: string): string {
  const lines = text.split('\n');
  const active = new RegExp(`^(?:export\s+)?${key}=`);
  const commented = new RegExp(`^#\s*${key}=`);
  let i = -1;
  lines.forEach((l, j) => {
    if (active.test(l)) i = j;
  });
  if (i < 0) i = lines.findIndex((l) => commented.test(l));
  const line = `${key}=${oneLine(value)}`;
  if (i >= 0) lines[i] = line;
  else if (lines.length && lines[lines.length - 1] === '') lines.splice(lines.length - 1, 0, line);
  else lines.push(line);
  return lines.join('\n');
}

/** No line breaks: one value must stay one line of the env file. */
export const oneLine = (v: unknown) => String(v ?? '').replace(/[\r\n]/g, '').trim();

/** Random [A-Za-z0-9] string from the Web Crypto API, without modulo bias. */
export function randomSecret(n: number): string {
  const abc = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  let out = '';
  const buf = new Uint8Array(n * 2);
  while (out.length < n) {
    crypto.getRandomValues(buf);
    for (const b of buf) if (b < 248 && out.length < n) out += abc[b % 62];
  }
  return out;
}

/** True for values the app refuses in production (empty or change_me style placeholders). */
export const isPlaceholder = (v: string | null) => !v || /change[_-]?me/i.test(v);
