// Ervisio Logs plugin (Ervisio plugin SDK v3, ADR 0012). Plain ES module, no build step: the page is drawn with DOM
// calls through the `{ render(container, sdk) }` form of registerPage.
//
// The administrator picks how Ervisio Logs runs on this server:
//   docker      the plugin writes /opt/stacks/ervisio-logs/{compose.yaml,.env} and runs its own `docker compose`
//               commands with the same argv as the Docker plugin, so the stack also appears there as "ervisio-logs".
//   standalone  the plugin downloads the release tarball it was built for, checks its SHA-256 and runs install.sh
//               (systemd service, /etc/ervisio-logs/ervisio-logs.env).
// The plugin never deletes data: no `down -v`, no volume or database removal, and Ervisio has no uninstall hook.
//
// scripts/release.sh replaces the BUNDLE line below with the release version and the Compose bundle.
const BUNDLE = { version: '0.0.0-dev', compose: null, envExample: null }; // @bundle

const CONFIG_DIR = '~/.config/ervisio/plugins/ervisio-logs';
const SETTINGS = `${CONFIG_DIR}/settings.json`;
const STACK_DIR = '/opt/stacks/ervisio-logs';
const STANDALONE_ENV = '/etc/ervisio-logs/ervisio-logs.env';

const STRINGS = {
  en: {
    'title': 'Ervisio Logs',
    'subtitle': 'Log search and archive for this server. Plugin {v}.',
    'devBuild': 'This is a development copy of the plugin: it carries no release, so it cannot install anything. Install a released plugin package (.tar.gz).',
    'mode.title': 'How should Ervisio Logs run here?',
    'mode.docker': 'Docker stack',
    'mode.docker.text': 'App, PostgreSQL and ClickHouse in containers, managed as the stack "ervisio-logs" in /opt/stacks. It also shows up in the Docker plugin.',
    'mode.standalone': 'Standalone service',
    'mode.standalone.text': 'One binary run by systemd. You provide PostgreSQL 15+ and ClickHouse 25.8, ideally on this machine.',
    'mode.checking': 'Checking…',
    'mode.dockerOk': 'Docker Compose {v} found',
    'mode.dockerMissing': 'Docker Compose v2 not found',
    'mode.use': 'Use this',
    'mode.change': 'Change mode',
    'mode.changeConfirm': 'Switching mode does not move or delete anything: the current installation and its data stay where they are. Continue?',
    'status': 'Status',
    'status.refresh': 'Refresh',
    'status.notInstalled': 'Not installed',
    'status.running': 'Running',
    'status.stopped': 'Stopped',
    'status.healthy': 'healthy',
    'status.unhealthy': 'not answering',
    'status.version': 'Installed version: {v}',
    'act.install': 'Install',
    'act.update': 'Update to {v}',
    'act.start': 'Start',
    'act.restart': 'Restart',
    'act.stop': 'Stop',
    'act.logs': 'Show logs',
    'act.open': 'Open Ervisio Logs',
    'act.uninstall': 'Remove service',
    'act.save': 'Save settings',
    'act.confirm': 'Continue',
    'act.cancel': 'Cancel',
    'act.saved': 'Settings saved. Restart to apply them.',
    'out': 'Output',
    'set.title': 'Settings',
    'set.publicUrl': 'Public URL',
    'set.publicUrl.help': 'The address people use to reach Ervisio Logs, through your reverse proxy. Production needs https.',
    'set.envMode': 'Environment',
    'set.port': 'Local port',
    'set.trustProxy': 'Behind a reverse proxy that sets X-Forwarded-For',
    'set.pgDsn': 'PostgreSQL DSN',
    'set.chAddr': 'ClickHouse address',
    'set.chUser': 'ClickHouse user',
    'set.chPassword': 'ClickHouse password',
    'set.secretKeep': 'Leave empty to keep the current value',
    'set.secretSet': 'set',
    'set.secretEmpty': 'empty',
    'set.noFile': 'Install first: the settings file does not exist yet.',
    'docker.note': 'The plugin rewrites compose.yaml on each install or update. Keep your own changes in .env.',
    'docker.inDockerPlugin': 'Manage the containers in the Docker plugin too: Stacks › ervisio-logs.',
    'docker.secrets': 'New installs get random database passwords and secret key in .env. The plugin never shows them.',
    'standalone.placeholders': 'After the first install, fill in the PostgreSQL DSN, the ClickHouse password and the public URL below, then start the service.',
    'uninstall.confirm': 'This stops and removes the ervisio-logs service and binary. Configuration, spool, cache and databases stay, and the output lists them. Continue?',
    'uninstall.docker': 'To remove the Docker stack, use Stop: it removes the containers and keeps the volumes. The plugin never deletes volumes. Delete them in the Docker plugin when you no longer need the data.',
    'removeNote': 'Removing this plugin from Ervisio leaves Ervisio Logs installed and running. Stop or remove it here first if you want it gone.',
    'setupCode': 'The first administrator signs in with a setup code. Find it in the logs ("setup code") or set ERVISIO_LOGS_SETUP_CODE.',
    'err.noUrl': 'Set the public URL first.',
    'err.exit': '{cmd} exited with code {n}',
  },
  it: {
    'title': 'Ervisio Logs',
    'subtitle': 'Ricerca e archivio dei log per questo server. Plugin {v}.',
    'devBuild': 'Questa è una copia di sviluppo del plugin: non contiene una release, quindi non può installare nulla. Installa il pacchetto .tar.gz di un plugin rilasciato.',
    'mode.title': 'Come deve girare Ervisio Logs qui?',
    'mode.docker': 'Stack Docker',
    'mode.docker.text': 'App, PostgreSQL e ClickHouse in container, gestiti come stack "ervisio-logs" in /opt/stacks. Compare anche nel plugin Docker.',
    'mode.standalone': 'Servizio standalone',
    'mode.standalone.text': 'Un solo binario gestito da systemd. PostgreSQL 15+ e ClickHouse 25.8 li fornisci tu, meglio se su questa macchina.',
    'mode.checking': 'Verifica…',
    'mode.dockerOk': 'Docker Compose {v} trovato',
    'mode.dockerMissing': 'Docker Compose v2 non trovato',
    'mode.use': 'Usa questo',
    'mode.change': 'Cambia modalità',
    'mode.changeConfirm': 'Cambiare modalità non sposta e non cancella nulla: l\'installazione attuale e i suoi dati restano dove sono. Continuare?',
    'status': 'Stato',
    'status.refresh': 'Aggiorna',
    'status.notInstalled': 'Non installato',
    'status.running': 'In esecuzione',
    'status.stopped': 'Fermo',
    'status.healthy': 'risponde',
    'status.unhealthy': 'non risponde',
    'status.version': 'Versione installata: {v}',
    'act.install': 'Installa',
    'act.update': 'Aggiorna a {v}',
    'act.start': 'Avvia',
    'act.restart': 'Riavvia',
    'act.stop': 'Ferma',
    'act.logs': 'Mostra log',
    'act.open': 'Apri Ervisio Logs',
    'act.uninstall': 'Rimuovi servizio',
    'act.save': 'Salva impostazioni',
    'act.confirm': 'Continua',
    'act.cancel': 'Annulla',
    'act.saved': 'Impostazioni salvate. Riavvia per applicarle.',
    'out': 'Output',
    'set.title': 'Impostazioni',
    'set.publicUrl': 'URL pubblico',
    'set.publicUrl.help': 'L\'indirizzo con cui si raggiunge Ervisio Logs, attraverso il reverse proxy. In produzione serve https.',
    'set.envMode': 'Ambiente',
    'set.port': 'Porta locale',
    'set.trustProxy': 'Dietro un reverse proxy che imposta X-Forwarded-For',
    'set.pgDsn': 'DSN PostgreSQL',
    'set.chAddr': 'Indirizzo ClickHouse',
    'set.chUser': 'Utente ClickHouse',
    'set.chPassword': 'Password ClickHouse',
    'set.secretKeep': 'Lascia vuoto per mantenere il valore attuale',
    'set.secretSet': 'impostata',
    'set.secretEmpty': 'vuota',
    'set.noFile': 'Prima installa: il file delle impostazioni non esiste ancora.',
    'docker.note': 'Il plugin riscrive compose.yaml a ogni installazione o aggiornamento. Tieni le tue modifiche in .env.',
    'docker.inDockerPlugin': 'Puoi gestire i container anche dal plugin Docker: Stack › ervisio-logs.',
    'docker.secrets': 'Una nuova installazione riceve in .env password dei database e chiave segreta casuali. Il plugin non le mostra mai.',
    'standalone.placeholders': 'Dopo la prima installazione compila qui sotto DSN PostgreSQL, password ClickHouse e URL pubblico, poi avvia il servizio.',
    'uninstall.confirm': 'Ferma e rimuove il servizio ervisio-logs e il binario. Configurazione, spool, cache e database restano, e l\'output li elenca. Continuare?',
    'uninstall.docker': 'Per rimuovere lo stack Docker usa Ferma: rimuove i container e tiene i volumi. Il plugin non cancella mai i volumi. Eliminali dal plugin Docker quando i dati non servono più.',
    'removeNote': 'Rimuovere questo plugin da Ervisio lascia Ervisio Logs installato e in esecuzione. Se vuoi toglierlo, fermalo o rimuovilo prima da qui.',
    'setupCode': 'Il primo amministratore accede con un codice di setup. Lo trovi nei log ("setup code") oppure impostando ERVISIO_LOGS_SETUP_CODE.',
    'err.noUrl': 'Imposta prima l\'URL pubblico.',
    'err.exit': '{cmd} è uscito con codice {n}',
  },
};

const CSS = `
.el { font: 14px/1.5 system-ui, sans-serif; color: var(--ink, inherit); max-width: 960px; padding: 20px; display: grid; gap: 18px; }
.el h1 { font-size: 22px; margin: 0; }
.el h2 { font-size: 16px; margin: 0 0 10px; }
.el p { margin: 0; }
.el .muted { color: var(--ink2, #667); font-size: 13px; }
.el .box { background: var(--surface, rgba(127,127,127,.06)); border-radius: var(--r-card, 14px); padding: 16px; display: grid; gap: 10px; }
.el .choices { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 14px; }
.el .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
.el button { font: inherit; padding: 7px 14px; border-radius: 10px; border: 1px solid var(--line, rgba(127,127,127,.35)); background: var(--sunk, transparent); color: inherit; cursor: pointer; }
.el button.primary { background: var(--acc, #2563eb); border-color: var(--acc, #2563eb); color: var(--on-acc, #fff); }
.el button.danger { color: var(--err, #c0392b); }
.el button:disabled { opacity: .5; cursor: default; }
.el button:focus-visible, .el input:focus-visible, .el select:focus-visible { outline: 2px solid var(--acc, #2563eb); outline-offset: 2px; }
.el .state { font-weight: 600; }
.el .ok { color: var(--ok, #1e8e3e); }
.el .bad { color: var(--err, #c0392b); }
.el table { border-collapse: collapse; width: 100%; font-size: 13px; }
.el td, .el th { text-align: left; padding: 4px 8px; border-bottom: 1px solid var(--line, rgba(127,127,127,.2)); }
.el pre { margin: 0; max-height: 320px; overflow: auto; background: var(--sunk, rgba(127,127,127,.1)); border-radius: 10px; padding: 10px; font: 12px/1.45 ui-monospace, monospace; white-space: pre-wrap; word-break: break-all; }
.el label { display: grid; gap: 4px; font-size: 13px; }
.el label.check { display: flex; gap: 8px; align-items: center; }
.el input, .el select { font: inherit; padding: 6px 10px; border-radius: 8px; border: 1px solid var(--line, rgba(127,127,127,.35)); background: var(--bg, transparent); color: inherit; }
.el .form { display: grid; gap: 12px; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); }
`;

/* ---------- helpers ---------- */

function h(tag, props, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (k in el && typeof v !== 'string') el[k] = v;
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const c of children.flat()) if (c != null && c !== false) el.append(c instanceof Node ? c : String(c));
  return el;
}

const isNotFound = (e) => e?.code === 'not_found' || /no such file|not exist|not found|ENOENT/i.test(e?.message ?? '');

/** Random [A-Za-z0-9] string from the Web Crypto API, without modulo bias. */
function randomSecret(n) {
  const abc = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  let out = '';
  const buf = new Uint8Array(n * 2);
  while (out.length < n) {
    crypto.getRandomValues(buf);
    for (const b of buf) if (b < 248 && out.length < n) out += abc[b % 62];
  }
  return out;
}

/** An env file as lines, edited in place so comments and order survive. */
const Env = {
  get(text, key) {
    let v = null;
    for (const line of text.split('\n')) {
      const m = line.match(/^(?:export\s+)?([A-Z0-9_]+)=(.*)$/);
      if (m && m[1] === key) v = m[2];
    }
    return v;
  },
  /** Sets key=value: replaces the last active line, else uncomments "# key=", else appends. */
  set(text, key, value) {
    const lines = text.split('\n');
    const active = (l) => new RegExp(`^(?:export\\s+)?${key}=`).test(l);
    let i = -1;
    lines.forEach((l, j) => { if (active(l)) i = j; });
    if (i < 0) i = lines.findIndex((l) => new RegExp(`^#\\s*${key}=`).test(l));
    const line = `${key}=${value}`;
    if (i >= 0) lines[i] = line;
    else {
      if (lines.length && lines[lines.length - 1] === '') lines.splice(lines.length - 1, 0, line);
      else lines.push(line);
    }
    return lines.join('\n');
  },
};

/** No line breaks: one value must stay one line of the env file. */
const oneLine = (v) => String(v ?? '').replace(/[\r\n]/g, '').trim();

/* ---------- the page ---------- */

function page(container, sdk) {
  const t = (k, vars) => sdk.t(k, vars);
  const api = sdk.api;
  const isRelease = !!(BUNDLE.compose && BUNDLE.envExample && BUNDLE.version !== '0.0.0-dev');

  let settings = { mode: null };
  let busy = false;
  let output = '';
  let status = null;
  let envText = null; // the active env file (docker .env or standalone env), null when missing
  let dockerCheck = null;
  let error = '';
  let pending = null; // { text, fn }: an action waiting for the inline confirmation (the sandbox blocks confirm())

  const style = h('style', {}, CSS);
  const root = h('div', { class: 'el' });
  container.replaceChildren(style, root);

  async function loadSettings() {
    try {
      settings = { mode: null, ...JSON.parse(await sdk.files.read(SETTINGS)) };
    } catch (e) {
      if (!isNotFound(e)) error = e.message;
    }
  }
  async function saveSettings() {
    await sdk.files.write(SETTINGS, JSON.stringify(settings, null, 2) + '\n');
  }

  function toastErr(e) {
    error = e?.message || String(e);
    sdk.ui?.toast?.err?.(error);
    draw();
  }

  /** Runs a declared command, streaming its output into the panel. Rejects on a non-zero exit. */
  function run(cmd) {
    output += `$ ${cmd}\n`;
    draw();
    return new Promise((resolve, reject) => {
      api.execStream(cmd, [], {
        onLine: (_s, line) => { output += line + '\n'; drawOutput(); },
        onExit: (code) => (code === 0 ? resolve() : reject(new Error(t('err.exit', { cmd, n: code })))),
        onError: reject,
      });
    });
  }

  async function action(fn) {
    if (busy) return;
    busy = true;
    error = '';
    output = '';
    draw();
    try {
      await fn();
    } catch (e) {
      toastErr(e);
    } finally {
      busy = false;
      await refresh();
    }
  }

  /* ----- docker mode ----- */

  async function readText(path) {
    try { return await sdk.files.read(path); } catch (e) { if (isNotFound(e)) return null; throw e; }
  }

  async function dockerStatus() {
    envText = await readText(`${STACK_DIR}/.env`);
    const compose = await readText(`${STACK_DIR}/compose.yaml`);
    if (compose == null) return { installed: false };
    const r = await api.exec('compose-ps');
    if (r.exitCode !== 0) return { installed: true, services: [], err: r.stderr.trim() };
    // Compose prints a JSON array (older v2) or one JSON object per line (newer).
    const text = r.stdout.trim();
    let rows = [];
    try {
      rows = text.startsWith('[') ? JSON.parse(text) : text.split('\n').filter(Boolean).map((l) => JSON.parse(l));
    } catch { /* leave empty */ }
    const deployedVersion = (compose.match(/ervisio-logs:([0-9][0-9A-Za-z.+-]*)\}/) || [])[1] || '';
    return {
      installed: true,
      deployedVersion,
      services: rows.map((s) => ({ name: s.Service, state: s.State, health: s.Health || '', status: s.Status || '' })),
    };
  }

  async function dockerInstall() {
    if (!isRelease) throw new Error(t('devBuild'));
    let exists = true;
    try { await sdk.files.list(STACK_DIR); } catch (e) { if (isNotFound(e)) exists = false; else throw e; }
    if (!exists) {
      await run('stacks-init');
      await run('stack-init');
    }
    await sdk.files.write(`${STACK_DIR}/compose.yaml`, BUNDLE.compose);
    output += `wrote ${STACK_DIR}/compose.yaml\n`;
    let env = await readText(`${STACK_DIR}/.env`);
    if (env == null) {
      env = BUNDLE.envExample;
      env = Env.set(env, 'POSTGRES_PASSWORD', randomSecret(32));
      env = Env.set(env, 'CLICKHOUSE_PASSWORD', randomSecret(32));
      env = Env.set(env, 'ERVISIO_LOGS_SECRET_KEY', randomSecret(48));
      output += `created ${STACK_DIR}/.env with random secrets\n`;
    } else {
      for (const [k, n] of [['POSTGRES_PASSWORD', 32], ['CLICKHOUSE_PASSWORD', 32], ['ERVISIO_LOGS_SECRET_KEY', 48]]) {
        if (Env.get(env, k) == null) { env = Env.set(env, k, randomSecret(n)); output += `added ${k} to .env\n`; }
      }
    }
    await sdk.files.write(`${STACK_DIR}/.env`, env.endsWith('\n') ? env : env + '\n');
    await run('compose-pull');
    await run('compose-up');
  }

  /* ----- standalone mode ----- */

  async function standaloneStatus() {
    envText = await readText(STANDALONE_ENV).catch(() => null);
    const r = await api.exec('standalone-status');
    const props = Object.fromEntries(r.stdout.split('\n').filter(Boolean).map((l) => l.split('=')));
    const installed = props.LoadState === 'loaded';
    let version = '';
    let healthy = false;
    if (installed) {
      const v = await api.exec('standalone-version').catch(() => null);
      version = v && v.exitCode === 0 ? v.stdout.trim().split(/\s+/)[1] || v.stdout.trim() : '';
      if (props.ActiveState === 'active') {
        const hc = await api.exec('standalone-health').catch(() => null);
        healthy = !!hc && hc.exitCode === 0;
      }
    }
    return { installed, active: props.ActiveState === 'active', sub: props.SubState || '', version, healthy };
  }

  /* ----- refresh and draw ----- */

  async function refresh() {
    try {
      if (settings.mode === 'docker') status = await dockerStatus();
      else if (settings.mode === 'standalone') status = await standaloneStatus();
      else if (dockerCheck == null) {
        dockerCheck = 'checking';
        draw();
        const r = await api.exec('docker-check').catch(() => null);
        dockerCheck = r && r.exitCode === 0 ? r.stdout.trim() : '';
      }
    } catch (e) {
      error = e?.message || String(e);
    }
    draw();
  }

  let outPre = null;
  function drawOutput() {
    if (outPre) { outPre.textContent = output; outPre.scrollTop = outPre.scrollHeight; } else draw();
  }

  function publicUrl() {
    return envText ? oneLine(Env.get(envText, 'ERVISIO_LOGS_PUBLIC_URL')) : '';
  }

  function openUi() {
    const url = publicUrl();
    if (!/^https?:\/\//.test(url)) return toastErr(new Error(t('err.noUrl')));
    sdk.openExternal(url).catch(toastErr);
  }

  function ask(text, fn) {
    pending = { text, fn };
    draw();
  }

  function btn(label, onclick, cls) {
    return h('button', { class: cls, disabled: busy, onclick }, label);
  }

  function chooseMode() {
    const dockerLine = dockerCheck === 'checking' ? t('mode.checking')
      : dockerCheck ? t('mode.dockerOk', { v: dockerCheck }) : t('mode.dockerMissing');
    const pick = (mode) => action(async () => { settings = { ...settings, mode }; await saveSettings(); });
    return h('section', {}, h('h2', {}, t('mode.title')), h('div', { class: 'choices' },
      h('div', { class: 'box' },
        h('h2', {}, t('mode.docker')), h('p', {}, t('mode.docker.text')),
        h('p', { class: dockerCheck && dockerCheck !== 'checking' ? 'ok' : 'muted' }, dockerLine),
        h('div', { class: 'row' }, btn(t('mode.use'), () => pick('docker'), 'primary'))),
      h('div', { class: 'box' },
        h('h2', {}, t('mode.standalone')), h('p', {}, t('mode.standalone.text')),
        h('div', { class: 'row' }, btn(t('mode.use'), () => pick('standalone'), 'primary')))));
  }

  function statusBox() {
    const s = status || {};
    const rows = [];
    let summary;
    if (!s.installed) summary = h('span', { class: 'state muted' }, t('status.notInstalled'));
    else if (settings.mode === 'docker') {
      const app = (s.services || []).find((x) => x.name === 'app');
      const up = app && app.state === 'running';
      summary = h('span', { class: `state ${up ? 'ok' : 'bad'}` }, up ? t('status.running') : t('status.stopped'),
        app?.health ? ` · ${app.health}` : '');
      if (s.services?.length) {
        rows.push(h('table', {}, h('tr', {}, h('th', {}, 'service'), h('th', {}, 'state'), h('th', {}, 'status')),
          s.services.map((x) => h('tr', {}, h('td', {}, x.name), h('td', {}, x.state, x.health ? ` (${x.health})` : ''), h('td', {}, x.status)))));
      }
      if (s.err) rows.push(h('p', { class: 'bad' }, s.err));
      if (s.deployedVersion) rows.push(h('p', { class: 'muted' }, t('status.version', { v: s.deployedVersion })));
    } else {
      summary = h('span', { class: `state ${s.active ? 'ok' : 'bad'}` }, s.active ? t('status.running') : t('status.stopped'),
        s.active ? ` · ${s.healthy ? t('status.healthy') : t('status.unhealthy')}` : s.sub ? ` · ${s.sub}` : '');
      if (s.version) rows.push(h('p', { class: 'muted' }, t('status.version', { v: s.version })));
    }
    return h('section', { class: 'box' },
      h('div', { class: 'row' }, h('h2', { style: 'margin:0' }, t('status')), summary,
        h('span', { style: 'flex:1' }), btn(t('status.refresh'), () => action(async () => {}))),
      rows);
  }

  function actions() {
    const s = status || {};
    const installLabel = s.installed ? t('act.update', { v: BUNDLE.version }) : t('act.install');
    const list = [];
    if (settings.mode === 'docker') {
      list.push(btn(installLabel, () => action(dockerInstall), 'primary'));
      if (s.installed) {
        list.push(btn(t('act.start'), () => action(() => run('compose-up'))));
        list.push(btn(t('act.stop'), () => action(() => run('compose-down'))));
        list.push(btn(t('act.logs'), () => action(() => run('compose-logs'))));
      }
    } else {
      list.push(btn(installLabel, () => action(() => { if (!isRelease) throw new Error(t('devBuild')); return run('standalone-install'); }), 'primary'));
      if (s.installed) {
        list.push(btn(s.active ? t('act.restart') : t('act.start'), () => action(() => run(s.active ? 'standalone-restart' : 'standalone-start'))));
        if (s.active) list.push(btn(t('act.stop'), () => action(() => run('standalone-stop'))));
        list.push(btn(t('act.logs'), () => action(() => run('standalone-logs'))));
        list.push(btn(t('act.uninstall'), () => ask(t('uninstall.confirm'), () => action(() => run('standalone-uninstall'))), 'danger'));
      }
    }
    if (s.installed) list.push(btn(t('act.open'), openUi));
    return h('div', { class: 'row' }, list);
  }

  function settingsForm() {
    if (envText == null) return h('section', { class: 'box' }, h('h2', {}, t('set.title')), h('p', { class: 'muted' }, t('set.noFile')));
    const fields = settings.mode === 'docker'
      ? [
          { key: 'ERVISIO_LOGS_PUBLIC_URL', label: t('set.publicUrl'), help: t('set.publicUrl.help') },
          { key: 'ERVISIO_LOGS_ENV', label: t('set.envMode'), options: ['development', 'production'] },
          { key: 'ERVISIO_LOGS_PORT', label: t('set.port'), pattern: '[0-9]{2,5}' },
          { key: 'ERVISIO_LOGS_TRUST_PROXY', label: t('set.trustProxy'), bool: true },
        ]
      : [
          { key: 'ERVISIO_LOGS_PUBLIC_URL', label: t('set.publicUrl'), help: t('set.publicUrl.help') },
          { key: 'ERVISIO_LOGS_POSTGRES_DSN', label: t('set.pgDsn'), secret: true },
          { key: 'ERVISIO_LOGS_CLICKHOUSE_ADDR', label: t('set.chAddr') },
          { key: 'ERVISIO_LOGS_CLICKHOUSE_USER', label: t('set.chUser') },
          { key: 'ERVISIO_LOGS_CLICKHOUSE_PASSWORD', label: t('set.chPassword'), secret: true },
          { key: 'ERVISIO_LOGS_TRUST_PROXY', label: t('set.trustProxy'), bool: true },
        ];
    const inputs = {};
    const nodes = fields.map((f) => {
      const cur = Env.get(envText, f.key) ?? '';
      let input;
      if (f.bool) {
        input = h('input', { type: 'checkbox', checked: cur === 'true' });
        inputs[f.key] = () => (input.checked ? 'true' : 'false');
        return h('label', { class: 'check' }, input, f.label);
      }
      if (f.options) {
        input = h('select', {}, f.options.map((o) => h('option', { value: o, selected: o === cur }, o)));
        inputs[f.key] = () => input.value;
      } else if (f.secret) {
        // The current value is never put in the page: an empty field keeps it.
        input = h('input', { type: 'password', autocomplete: 'off', placeholder: t('set.secretKeep') });
        inputs[f.key] = () => (input.value ? oneLine(input.value) : null);
      } else {
        input = h('input', { type: 'text', value: cur, pattern: f.pattern, spellcheck: false });
        inputs[f.key] = () => oneLine(input.value);
      }
      const hint = f.secret ? `${f.label} (${/CHANGE_ME/.test(cur) || !cur ? t('set.secretEmpty') : t('set.secretSet')})` : f.label;
      return h('label', {}, hint, input, f.help ? h('span', { class: 'muted' }, f.help) : null);
    });
    const path = settings.mode === 'docker' ? `${STACK_DIR}/.env` : STANDALONE_ENV;
    const save = () => action(async () => {
      let text = await readText(path);
      if (text == null) throw new Error(t('set.noFile'));
      for (const [k, get] of Object.entries(inputs)) {
        const v = get();
        if (v != null) text = Env.set(text, k, v);
      }
      await sdk.files.write(path, text);
      sdk.ui?.toast?.ok?.(t('act.saved'));
    });
    return h('section', { class: 'box' }, h('h2', {}, t('set.title')), h('p', { class: 'muted' }, path),
      h('div', { class: 'form' }, nodes), h('div', { class: 'row' }, btn(t('act.save'), save, 'primary')));
  }

  function draw() {
    outPre = null;
    const head = h('header', { class: 'row' },
      h('div', { style: 'flex:1' }, h('h1', {}, t('title')), h('p', { class: 'muted' }, t('subtitle', { v: BUNDLE.version }))),
      settings.mode ? btn(t('mode.change'), () => ask(t('mode.changeConfirm'), () =>
        action(async () => { settings = { ...settings, mode: null }; status = null; dockerCheck = null; await saveSettings(); }))) : null);
    const parts = [head];
    if (pending) {
      const p = pending;
      parts.push(h('section', { class: 'box', role: 'alertdialog' }, h('p', {}, p.text), h('div', { class: 'row' },
        btn(t('act.confirm'), () => { pending = null; p.fn(); }, 'primary'),
        btn(t('act.cancel'), () => { pending = null; draw(); }))));
    }
    if (!isRelease) parts.push(h('p', { class: 'box bad' }, t('devBuild')));
    if (error) parts.push(h('p', { class: 'box bad', role: 'alert' }, error));
    if (!settings.mode) parts.push(chooseMode());
    else {
      parts.push(statusBox(), actions());
      const notes = settings.mode === 'docker'
        ? [t('docker.inDockerPlugin'), t('docker.secrets'), t('docker.note'), t('uninstall.docker')]
        : [t('standalone.placeholders')];
      parts.push(h('section', { class: 'box' }, [...notes, t('setupCode'), t('removeNote')].map((n) => h('p', { class: 'muted' }, n))));
      parts.push(settingsForm());
    }
    if (output) {
      outPre = h('pre', { 'aria-live': 'polite' }, output);
      parts.push(h('section', {}, h('h2', {}, t('out')), outPre));
      queueMicrotask(() => { if (outPre) outPre.scrollTop = outPre.scrollHeight; });
    }
    root.replaceChildren(...parts);
  }

  draw();
  loadSettings().then(refresh);
}

export default function activate(sdk) {
  sdk.registerStrings(STRINGS);
  sdk.registerPage('logs', { render: (container) => page(container, sdk) });
}
