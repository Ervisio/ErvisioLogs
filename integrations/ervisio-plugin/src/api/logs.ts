/**
 * Ervisio Logs on this server: the chosen mode (Docker stack or standalone service), its state, install and update,
 * the env file, and the address the reverse proxy reaches it at. Every command is declared in plugin/manifest.json.
 */
import { getSdk, type PluginError } from '../sdk';
import { BUNDLE } from '../bundle.generated';
import type { Upstream } from './caddy-core';
import { ensureSecrets, envGet, envSet, unpassedKeys } from './env';

export type Mode = 'docker' | 'standalone';

export const CONFIG_DIR = '~/.config/ervisio/plugins/ervisio-logs';
const PREFS = `${CONFIG_DIR}/settings.json`;
export const STACK_DIR = '/opt/stacks/ervisio-logs';
export const STANDALONE_ENV = '/etc/ervisio-logs/ervisio-logs.env';
/** The Docker network the app joins as ervisio-logs-app (deploy/compose.yaml), for a Caddy in Docker. */
export const EDGE_NETWORK = 'ervisiologs_edge';
export const APP_CONTAINER = 'ervisio-logs-app';

export { BUNDLE };
/** A released plugin carries the compose bundle and the standalone checksums; a dev checkout installs nothing. */
export const isRelease = !!(BUNDLE.compose && BUNDLE.envExample && BUNDLE.version !== '0.0.0-dev');

/* ---------- preferences ---------- */

export interface Prefs {
  mode: Mode | null;
  /** The user allowed the Docker unlock prompt to look for Caddy containers. */
  askDocker?: boolean;
}

const isNotFound = (e: unknown) => {
  const err = e as PluginError | undefined;
  return err?.code === 'not_found' || /no such file|not exist|not found|ENOENT/i.test(err?.message ?? '');
};

export async function loadPrefs(): Promise<Prefs> {
  try {
    const p = JSON.parse(await getSdk().files.read(PREFS)) as Partial<Prefs>;
    return { mode: p.mode === 'docker' || p.mode === 'standalone' ? p.mode : null, askDocker: !!p.askDocker };
  } catch (e) {
    if (isNotFound(e) || e instanceof SyntaxError) return { mode: null };
    throw e;
  }
}

export async function savePrefs(patch: Partial<Prefs>): Promise<Prefs> {
  const next = { ...(await loadPrefs()), ...patch };
  await getSdk().files.write(PREFS, JSON.stringify(next, null, 2) + '\n');
  return next;
}

/* ---------- commands ---------- */

export type Log = (line: string) => void;

export async function readText(path: string): Promise<string | null> {
  try {
    return await getSdk().files.read(path);
  } catch (e) {
    if (isNotFound(e)) return null;
    throw e;
  }
}

/** Runs a declared command, streaming its output to `log`. Rejects on a non-zero exit. */
export function run(cmd: string, log: Log, args: string[] = []): Promise<void> {
  log(`$ ${[cmd, ...args].join(' ')}`);
  return new Promise((resolve, reject) => {
    getSdk().api.execStream(cmd, args, {
      onLine: (_s, line) => log(line),
      onExit: (code) => (code === 0 ? resolve() : reject(new Error(getSdk().t('err.exit', { cmd, n: code })))),
      onError: (e) => reject(e),
    });
  });
}

export interface DockerCheck {
  ok: boolean;
  version: string;
}

/** Docker Compose v2 on this server (the same daemon the Docker plugin uses). */
export async function dockerCheck(): Promise<DockerCheck> {
  try {
    const r = await getSdk().api.exec('docker-check');
    return { ok: r.exitCode === 0, version: r.stdout.trim() };
  } catch {
    return { ok: false, version: '' };
  }
}

/* ---------- state ---------- */

export interface Service {
  name: string;
  state: string;
  health: string;
  status: string;
}

export interface Status {
  mode: Mode;
  installed: boolean;
  running: boolean;
  /** The app answers its health check (null when it is not running). */
  healthy: boolean | null;
  /** Deployed version ('' when unknown). */
  version: string;
  services: Service[];
  envPath: string;
  /** The env file, null when it does not exist yet. */
  env: string | null;
  publicUrl: string | null;
  /** Something went wrong reading the state (shown, not fatal). */
  error?: string;
}

export const envPathFor = (mode: Mode) => (mode === 'docker' ? `${STACK_DIR}/.env` : STANDALONE_ENV);

export async function status(mode: Mode): Promise<Status> {
  return mode === 'docker' ? dockerStatus() : standaloneStatus();
}

async function dockerStatus(): Promise<Status> {
  const envPath = envPathFor('docker');
  const [env, compose] = await Promise.all([readText(envPath), readText(`${STACK_DIR}/compose.yaml`)]);
  const base: Status = { mode: 'docker', installed: false, running: false, healthy: null, version: '', services: [], envPath, env, publicUrl: envGet(env, 'ERVISIO_LOGS_PUBLIC_URL') };
  if (compose == null) return base;
  base.installed = true;
  base.version = (compose.match(/ervisio-logs:([0-9][0-9A-Za-z.+-]*)\}/) || [])[1] || '';
  const r = await getSdk().api.exec('compose-ps').catch((e: Error) => ({ exitCode: 1, stdout: '', stderr: e.message }));
  if (r.exitCode !== 0) return { ...base, error: r.stderr.trim() };
  // Compose prints a JSON array (older v2) or one JSON object per line (newer).
  const text = r.stdout.trim();
  let rows: Array<Record<string, string>> = [];
  try {
    rows = text.startsWith('[') ? JSON.parse(text) : text.split('\n').filter(Boolean).map((l) => JSON.parse(l));
  } catch {
    /* leave empty */
  }
  const services = rows.map((s) => ({ name: s.Service ?? '', state: s.State ?? '', health: s.Health ?? '', status: s.Status ?? '' }));
  const app = services.find((s) => s.name === 'app');
  const running = app?.state === 'running';
  return { ...base, services, running, healthy: running ? app!.health === 'healthy' || app!.health === '' : null };
}

async function standaloneStatus(): Promise<Status> {
  const sdk = getSdk();
  const envPath = envPathFor('standalone');
  const env = await readText(envPath).catch(() => null);
  const base: Status = { mode: 'standalone', installed: false, running: false, healthy: null, version: '', services: [], envPath, env, publicUrl: envGet(env, 'ERVISIO_LOGS_PUBLIC_URL') };
  const r = await sdk.api.exec('standalone-status').catch(() => null);
  if (!r) return base;
  const props = Object.fromEntries(r.stdout.split('\n').filter(Boolean).map((l) => [l.slice(0, l.indexOf('=')), l.slice(l.indexOf('=') + 1)]));
  if (props.LoadState !== 'loaded') return base;
  const running = props.ActiveState === 'active';
  const v = await sdk.api.exec('standalone-version').catch(() => null);
  const version = v && v.exitCode === 0 ? v.stdout.trim().split(/\s+/)[1] || v.stdout.trim() : '';
  let healthy: boolean | null = null;
  if (running) {
    const hc = await sdk.api.exec('standalone-health').catch(() => null);
    healthy = !!hc && hc.exitCode === 0;
  }
  return {
    ...base,
    installed: true,
    running,
    healthy,
    version,
    services: [{ name: 'ervisio-logs.service', state: props.ActiveState ?? '', health: healthy ? 'healthy' : running ? 'unhealthy' : '', status: props.SubState ?? '' }],
  };
}

/** A newer release than the deployed one is bundled in this plugin. */
export const updateAvailable = (s: Status) => isRelease && s.installed && !!s.version && s.version !== BUNDLE.version;

/* ---------- install, update, lifecycle ---------- */

/** Docker: writes compose.yaml from the bundle, creates or completes .env, pulls and starts the stack. */
export async function dockerInstall(log: Log): Promise<void> {
  if (!isRelease) throw new Error(getSdk().t('devBuild'));
  const files = getSdk().files;
  let exists = true;
  try {
    await files.list(STACK_DIR);
  } catch (e) {
    if (isNotFound(e)) exists = false;
    else throw e;
  }
  if (!exists) {
    await run('stacks-init', log);
    await run('stack-init', log);
  }
  await files.write(`${STACK_DIR}/compose.yaml`, BUNDLE.compose!);
  log(`wrote ${STACK_DIR}/compose.yaml`);
  let env = await readText(`${STACK_DIR}/.env`);
  if (env == null) {
    env = ensureSecrets(BUNDLE.envExample!, () => {});
    log(`created ${STACK_DIR}/.env with random secrets`);
  } else {
    env = ensureSecrets(env, log);
    // 0.1.0 ran PostgreSQL without TLS; the bundled database now serves it, so a leftover sslmode=disable goes.
    if (envGet(env, 'POSTGRES_SSLMODE') === 'disable') {
      env = envSet(env, 'POSTGRES_SSLMODE', 'verify-full');
      log('switched POSTGRES_SSLMODE to verify-full (the bundled PostgreSQL serves TLS)');
    }
  }
  warnProduction('docker', env, log);
  const ignored = unpassedKeys(env, BUNDLE.compose);
  if (ignored.length) log(`warning: compose.yaml does not pass ${ignored.join(', ')} to the container: ${STACK_DIR}/.env values for them are ignored`);
  await files.write(`${STACK_DIR}/.env`, env.endsWith('\n') ? env : env + '\n');
  // compose.yaml mounts ./secrets; made here so Docker does not create it as root.
  await run('secrets-init', log);
  await run('compose-pull', log);
  await run('compose-up', log);
}

export async function standaloneInstall(log: Log): Promise<void> {
  if (!isRelease) throw new Error(getSdk().t('devBuild'));
  await run('standalone-install', log);
}

export const install = (mode: Mode, log: Log) => (mode === 'docker' ? dockerInstall(log) : standaloneInstall(log));

/** Applies a changed env file: Compose recreates the containers whose settings changed; systemd restarts the unit. */
export const restart = (mode: Mode, log: Log) => run(mode === 'docker' ? 'compose-up' : 'standalone-restart', log);
export const start = (mode: Mode, log: Log) => run(mode === 'docker' ? 'compose-up' : 'standalone-start', log);
/** Docker: removes the containers and keeps every volume. Standalone: stops the unit. */
export const stop = (mode: Mode, log: Log) => run(mode === 'docker' ? 'compose-down' : 'standalone-stop', log);
export const logs = (mode: Mode, log: Log) => run(mode === 'docker' ? 'compose-logs' : 'standalone-logs', log);
export const uninstallStandalone = (log: Log) => run('standalone-uninstall', log);

/* ---------- settings and production checks ---------- */

export type Problem = 'url-https' | 'url-invalid' | 'pg-ssl' | 'placeholder';

/**
 * What the app would refuse in production with this env file, the same rules as backend/internal/config: an https
 * public URL, no sslmode=disable, no change_me secrets. The Docker stack builds its DSN from POSTGRES_SSLMODE.
 */
export function productionProblems(mode: Mode, env: string): Problem[] {
  const out: Problem[] = [];
  const url = envGet(env, 'ERVISIO_LOGS_PUBLIC_URL');
  try {
    const u = new URL(url ?? '');
    if (u.protocol !== 'https:') out.push('url-https');
  } catch {
    out.push('url-invalid');
  }
  const dsn = envGet(env, 'ERVISIO_LOGS_POSTGRES_DSN');
  const sslmode = dsn ? new URLSearchParams(dsn.split('?')[1] ?? '').get('sslmode') : mode === 'docker' ? envGet(env, 'POSTGRES_SSLMODE') : null;
  if (sslmode === 'disable') out.push('pg-ssl');
  const secrets =
    mode === 'docker'
      ? ['POSTGRES_PASSWORD', 'CLICKHOUSE_PASSWORD', 'ERVISIO_LOGS_SECRET_KEY']
      : ['ERVISIO_LOGS_POSTGRES_DSN', 'ERVISIO_LOGS_CLICKHOUSE_PASSWORD', 'ERVISIO_LOGS_SECRET_KEY'];
  if (secrets.some((k) => /change_me/i.test(envGet(env, k) ?? ''))) out.push('placeholder');
  return out;
}

/** ERVISIO_LOGS_ENV is the user's choice and stays as it is; production the app would refuse is only reported. */
function warnProduction(mode: Mode, env: string, log: Log) {
  if (envGet(env, 'ERVISIO_LOGS_ENV') !== 'production') return;
  const p = productionProblems(mode, env);
  if (p.length) log(`warning: ERVISIO_LOGS_ENV=production but Ervisio Logs will refuse to start until these are fixed: ${p.join(', ')}`);
}

export async function readEnv(mode: Mode): Promise<string | null> {
  return readText(envPathFor(mode));
}

/** Docker: ERVISIO_LOGS_* values in .env that the installed compose.yaml does not pass to the container. */
export async function ignoredEnvKeys(mode: Mode, env: string | null): Promise<string[]> {
  if (mode !== 'docker') return [];
  return unpassedKeys(env, await readText(`${STACK_DIR}/compose.yaml`));
}

export async function writeEnv(mode: Mode, text: string): Promise<void> {
  await getSdk().files.write(envPathFor(mode), text.endsWith('\n') ? text : text + '\n');
}

/**
 * After Caddy serves `url`: the app gets it as its public address, trusts the proxy's X-Forwarded-For, switches to
 * production when nothing else blocks it, and restarts. Returns whether production mode is now on.
 */
export async function setPublicUrl(mode: Mode, url: string, log: Log): Promise<{ production: boolean; problems: Problem[] }> {
  let env = await readEnv(mode);
  if (env == null) throw new Error(getSdk().t('set.noFile'));
  env = envSet(env, 'ERVISIO_LOGS_PUBLIC_URL', url);
  env = envSet(env, 'ERVISIO_LOGS_TRUST_PROXY', 'true');
  const problems = productionProblems(mode, env);
  // Only ever switches production on: an environment the user chose is not turned back to development.
  if (!problems.length) env = envSet(env, 'ERVISIO_LOGS_ENV', 'production');
  await writeEnv(mode, env);
  log(`set ERVISIO_LOGS_PUBLIC_URL=${url} in ${envPathFor(mode)}`);
  await restart(mode, log);
  return { production: !problems.length, problems };
}

/* ---------- where the proxy reaches the app ---------- */

/**
 * Docker: the loopback port published by Compose, or the app container on the edge network. Standalone: the address
 * of ERVISIO_LOGS_HTTP_ADDR (default 127.0.0.1:8080); a Caddy container on a bridge network cannot reach it.
 */
export function upstream(mode: Mode, env: string | null): Upstream {
  if (mode === 'docker') {
    const port = Number(envGet(env, 'ERVISIO_LOGS_PORT')) || 8080;
    return { host: '127.0.0.1', port, container: APP_CONTAINER, containerPort: 8080, network: EDGE_NETWORK };
  }
  const addr = envGet(env, 'ERVISIO_LOGS_HTTP_ADDR') || '127.0.0.1:8080';
  const i = addr.lastIndexOf(':');
  const host = i > 0 ? addr.slice(0, i).replace(/^\[|\]$/g, '') : '127.0.0.1';
  const port = Number(addr.slice(i + 1)) || 8080;
  return { host: host || '127.0.0.1', port, container: '', containerPort: port, network: '' };
}

// Archive credentials (Storage page of Ervisio Logs). One file per destination; the page never reads one back.
export const SECRETS_DIR: Record<Mode, string> = { docker: `${STACK_DIR}/secrets`, standalone: '/etc/ervisio-logs/secrets' };
const STAGING: Record<Mode, string> = { docker: `${STACK_DIR}/.secret-incoming`, standalone: '/etc/ervisio-logs/.secret-incoming' };
const cmdFor = (mode: Mode, c: 'init' | 'install' | 'list') => (mode === 'docker' ? `secrets-${c}` : `standalone-secrets-${c}`);

/** The file names the backend accepts as secret_file, minus names that start with a dot. */
export const validSecretName = (n: string) => /^[A-Za-z0-9_-][A-Za-z0-9._-]{0,99}$/.test(n);

/** Names of the credential files, or [] when the folder does not exist yet. */
export async function listSecrets(mode: Mode): Promise<string[]> {
  const r = await getSdk().api.exec(cmdFor(mode, 'list')).catch(() => null);
  if (!r || r.exitCode !== 0) return [];
  return r.stdout.split('\n').map((l) => l.trim()).filter(validSecretName).sort();
}

/**
 * Writes one credential file: the value goes to a staging file inside the closed stack or config folder, then a root
 * command installs it with the owner and mode the app needs, and the staging file is removed. The value is never logged.
 */
export async function writeSecret(mode: Mode, name: string, value: string, log: Log): Promise<void> {
  if (!validSecretName(name)) throw new Error(getSdk().t('sec.badName'));
  const staging = STAGING[mode];
  await run(cmdFor(mode, 'init'), log);
  await getSdk().files.write(staging, value.endsWith('\n') ? value : value + '\n');
  try {
    await run(cmdFor(mode, 'install'), log, [`${SECRETS_DIR[mode]}/${name}`]);
  } finally {
    await getSdk().files.remove(staging).catch(() => log(`could not remove ${staging}; delete it by hand`));
  }
}
