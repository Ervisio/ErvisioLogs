/** Env files edited line by line, so comments and order survive, and random secrets for new installs. */

/** The `[key, raw value]` of every active `KEY=value` line, in file order. */
function entries(text: string | null): Array<[string, string]> {
  const out: Array<[string, string]> = [];
  for (const line of (text ?? '').split('\n')) {
    const m = line.replace(/\r$/, '').match(/^(?:export\s+)?([A-Z0-9_]+)=(.*)$/);
    if (m) out.push([m[1], m[2]]);
  }
  return out;
}

/** The value of the last active `KEY=` line, or null. */
export function envGet(text: string | null, key: string): string | null {
  let v: string | null = null;
  for (const [k, raw] of entries(text)) if (k === key) v = unquote(raw);
  return v;
}

/** The keys with an active `KEY=value` line and a non-empty value. */
export function envKeys(text: string | null): string[] {
  return [...new Set(entries(text).filter(([, raw]) => unquote(raw)).map(([k]) => k))];
}

/** App settings in the env file that compose.yaml never passes to the containers: Compose ignores them. */
export function unpassedKeys(env: string | null, compose: string | null): string[] {
  if (compose == null) return [];
  const passed = new Set([...compose.matchAll(/\$\{([A-Z0-9_]+)/g)].map((m) => m[1]));
  return envKeys(env).filter((k) => k.startsWith('ERVISIO_LOGS_') && !passed.has(k));
}

function unquote(v: string): string {
  const s = v.trim();
  if (s.length >= 2 && ((s[0] === '"' && s.endsWith('"')) || (s[0] === "'" && s.endsWith("'")))) return s.slice(1, -1);
  return s;
}

/** Sets KEY=value: replaces the last active line, else uncomments "# KEY=", else appends. */
export function envSet(text: string, key: string, value: string): string {
  const lines = text.split('\n');
  const active = new RegExp(`^(?:export\\s+)?${key}=`);
  const commented = new RegExp(`^#\\s*(?:export\\s+)?${key}=`);
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

/** Fills the secrets that are missing or still change_me; a secret that is already set is never replaced. */
export function ensureSecrets(env: string, log: (line: string) => void): string {
  for (const [k, n] of [['POSTGRES_PASSWORD', 32], ['CLICKHOUSE_PASSWORD', 32], ['ERVISIO_LOGS_SECRET_KEY', 48]] as const) {
    if (isPlaceholder(envGet(env, k))) {
      env = envSet(env, k, randomSecret(n));
      log(`set a random ${k} in .env`);
    }
  }
  return env;
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
