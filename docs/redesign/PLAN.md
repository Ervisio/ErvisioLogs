# Ervisio Logs 0.2: piano di redesign

Stato: approvato con modifiche (2026-11-04). Fonte: audit di spec vs codice, web app, plugin e modello dei log.

## Obiettivi

1. Ogni funzione che il backend ha già deve avere una UI. Le funzioni che mancano vengono costruite.
2. Una UI semplice: ogni impostazione ha un posto ovvio e uno solo, e ogni scope (istanza, workspace, progetto) ha la sua area.
3. Log leggibili da chi gestisce un server di gioco, non solo da chi lo sviluppa.
4. Dal plugin Ervisio si gestisce tutto: ambiente/istanza, workspace, progetti, utenti.
5. Zero "coming soon" nel menu: una voce appare solo quando la funzione esiste.

## Stato di partenza (sintesi audit)

- 18 bug verificati nella web app, 3 gravi: progetto di un altro workspace dà "Not found"; Token e Membri raggiungibili solo da una pagina vuota; scegliere un livello fa sparire gli altri chip.
- API senza UI: rename/delete di workspace e progetti, ambienti, stream, audit, admin utenti, PUT delle query salvate, scadenza token.
- Gli stream si creano solo via API, e l'ingest rifiuta gli stream inesistenti: gli stream di default della risorsa FiveM (`players`, `economy`...) vengono rifiutati.
- Tutte le impostazioni di istanza sono variabili d'ambiente, lette all'avvio. Nel Docker molte non arrivano nemmeno al container (`compose.yaml` passa solo una parte).
- Il plugin non può chiamare l'API: iframe senza cookie, `http` solo su unix socket, admin solo con sessione.
- I log: niente tag, niente attore/bersaglio, nomi evento tecnici (`money.add`), livelli in minuscolo, attributi stampati come `key value, ...`, ora senza data, badge "hot" sbagliato.
- Plugin: `envSet` non scommenta né riconosce `export` (regex in template literal, `\s` diventa `s`); il draft delle impostazioni si azzera a ogni refresh; `ENV` scritto a `development` di nascosto.

Dettaglio bug: sezione "Fase 1".

## Architettura dell'informazione (nuova)

URL con lo scope dentro: `/w/:workspace/p/:project/...`. Il workspace corrente è sempre visibile in alto nella rail, con il ruolo.

```
Rail (sempre)          Switcher workspace · progetti del workspace · Admin (solo admin istanza) · Docs · Account
Progetto               Log (explorer con Live come interruttore) · Avvisi · Dashboard
Impostazioni progetto  Generale · Ambienti · Stream · Token · Eventi e campi · Conservazione · Zona pericolosa
Impostazioni workspace Generale · Membri e inviti · Canali di notifica · Conservazione · Registro attività · Zona pericolosa
Amministrazione        Panoramica/salute · Utenti · Workspace · Archivio (destinazioni) · Conservazione predefinita
                       · Accesso (registrazione, OAuth, sessioni, MFA) · Email (SMTP) · Limiti ingest · Avvisi di sistema
                       · Registro attività · Avanzate (intervalli archiviatore, export, log level)
Account                Profilo · Password · MFA · Sessioni attive · Lingua e tema
```

Regole:
- Una sezione sta nello scope a cui appartiene: i canali di notifica e i membri sono del workspace, lo storage è dell'istanza.
- Le voci che l'utente non può usare non compaiono (oggi Storage appare a tutti e poi dice "solo admin").
- "Live" non è più una voce di menu, è un interruttore dell'explorer.
- Il palette ⌘K diventa "Vai a…" e cerca anche impostazioni.

## Impostazioni a tre livelli

Il backend passa da "solo env" a impostazioni in database, con questo ordine di precedenza:

1. Env impostata esplicitamente: vince e la UI la mostra bloccata ("impostata dal server").
2. Valore in DB (`settings`, chiave tipizzata, scope instance/workspace/project).
3. Default di codice.

Restano solo env (servono prima che il DB sia raggiungibile): DSN Postgres/ClickHouse, indirizzo di ascolto, percorsi (spool, cache, secrets), `SECRET_KEY`, `PUBLIC_URL`. Il plugin le modifica nel file env e riavvia.

Diventano runtime (DB, senza riavvio): registrazione aperta, durata sessione, provider OAuth (id e secret come file nel secrets dir, come gli archivi), SMTP, limiti ingest (skew, età massima, rate limit per token/progetto, quota), intervalli archiviatore, intervallo e storico avvisi, `WEBHOOK_ALLOW_PRIVATE`, max righe export, log level, token metriche.

Per workspace: nome, slug, conservazione, quota ingest, canali predefiniti per gli avvisi.
Per progetto: nome, slug, ambienti, stream (con nome visibile, descrizione, colore), creazione automatica degli stream (decisa dall'admin del progetto: sì/no, con limite o illimitata), catalogo eventi, regole di visualizzazione dei campi, conservazione, quota.

## Plugin: gestire tutto

Tre canali, scelti in base al caso:
- Socket admin locale (installazione sulla stessa macchina). Il backend apre un unix socket (`ERVISIO_LOGS_ADMIN_SOCKET`, 0660) che accetta `/admin/*` e la gestione tenancy senza sessione né CSRF; l'autorizzazione è il permesso sul file, come per docker.sock. Nel Docker il socket sta in un volume montato sull'host; il plugin lo usa con `capabilities.http`.
- Token admin d'istanza via HTTPS (istanza su un'altra macchina). Nuovo tipo di token (`evl_adm_`), creato da un admin, con scope `admin:*` limitati e scadenza. Solo header Bearer, mai cookie: CORS `*` senza credenziali sulle route admin, così l'iframe del plugin (origine opaca) può chiamarle. Il plugin lo conserva nei secrets dell'SDK.
- File env + riavvio, solo per l'infrastruttura (DSN, porta, percorsi, `SECRET_KEY`, `PUBLIC_URL`).

Ogni azione arrivata da socket o token admin finisce nel registro attività con attore `plugin` o il nome del token.

Schede del plugin:
```
Panoramica   stato servizi, salute (WAL, archivio, coda upload), aggiornamenti, link alla web UI
Istanza      tutte le impostazioni runtime raggruppate come in Amministrazione
Workspace    elenco, crea, rinomina, elimina, membri, quota, conservazione
Progetti     per workspace: ambienti, stream, token, conservazione
Utenti       elenco e ricerca, crea, disabilita, elimina, promuovi/revoca admin, reset password, chiudi sessioni
Archivio     destinazioni e credenziali (oggi in Secrets)
Server       env di infrastruttura (DSN, porta, percorsi), con riavvio guidato
Indirizzo    Caddy, come oggi
Console      storico dei job, non solo l'ultimo
```
La web UI e il plugin usano gli stessi endpoint. Un client TypeScript condiviso (`packages/admin-client`) evita di scrivere due volte tipi e chiamate; le due UI restano separate perché il plugin usa il kit dell'SDK.

## Log v2

Modello (tutto opzionale e retrocompatibile; server aggiornato prima dei client, l'ingest rifiuta i campi sconosciuti):
- `tags`: array (max 20, 40 caratteri, minuscolo), colonna ClickHouse `Array(LowCardinality(String))` con indice bloom filter. Filtro `tag:x`.
- `actor` e `target`: convenzione in `attributes.actor` / `attributes.target` = `{type, id, name}` (player, vehicle, resource, item, admin). Filtrabili già oggi via path; colonne vere solo se i volumi lo chiedono.
- Catalogo eventi per progetto (Postgres `event_types`): nome evento → titolo, frase modello (`{actor.name} ha ricevuto {amount} € ({account})`), icona, categoria, colore, campi in evidenza. Popolato in automatico dai nomi visti; preset FiveM incluso.
- Stream con nome visibile, descrizione, colore; creazione automatica per progetto (on/off, limite opzionale).
- Livelli con etichette e colori distinti per tutti e sette (Traccia, Debug, Info, Nota, Attenzione, Errore, Critico).

Explorer:
- Riga: ora relativa ("3 min fa", data completa al passaggio), pallino livello, icona e titolo dell'evento dal catalogo, frase leggibile, 2-3 chip (attore, bersaglio, tag). Separatore quando cambia il giorno. Il nome tecnico dell'evento solo nel dettaglio.
- Istogramma nel tempo sopra la lista, con selezione di un intervallo trascinando.
- Pannello filtri: livelli (sempre tutti visibili, con conteggi), eventi, categorie, tag, stream, risorsa, giocatore. Ogni valore nel dettaglio è cliccabile e diventa filtro.
- Costruttore di filtri (campo, operatore, valore) che scrive la query testuale; la sintassi resta per chi la vuole.
- Dettaglio raggruppato: Chi / Cosa / Dove / Quando / Tecnico, JSON grezzo chiuso sotto "Mostra originale".
- Lista virtualizzata; live sempre attivabile (il tail impara a filtrare gli attributi).
- Badge "Archivio" solo sulle righe che vengono dall'archivio.

## Fasi

Ogni fase è una PR (o più), con test, rilascio e note di versione.

### Fase 0: design system
- `/designer`: riferimenti reali (strumenti di log e pannelli di gestione), token (colori, tipografia, spaziature, densità), componenti base.
- `/variant-studio` sulle schermate chiave: shell e navigazione, riga e dettaglio log, pagina impostazioni, admin utenti, plugin.
- `/stop-slop` su tutti i testi dell'interfaccia (it ed en).
- Uscita: `DESIGN.md`, mock HTML approvati, token in `web/src/index.css`.

### Fase 1: bug (subito, prima del redesign)
Web: progetto cross-workspace in URL; Token/Membri raggiungibili; conteggi livelli calcolati senza il filtro livello; badge tier per riga; data nelle righe; modifica avviso che perde ambienti e stream; viste salvate senza ambiente; slug non ASCII; live che resta su "connessione…" dopo un 400; doppio conteggio nel footer; "carica altro" che sposta l'inizio dell'intervallo; range da sessionStorage non validato; Export visibile senza permesso; 503 avvisi mostrato come errore generico; ruoli non aggiornati dopo la login; Storage visibile ai non admin; dettaglio log senza focus trap; token del primo evento tutti con lo stesso nome.
Plugin: `envSet` (regex), draft azzerato dal refresh, `ENV` scritto di nascosto, valori `.env` ignorati nel Docker senza avviso, verifica che l'update non rigeneri `SECRET_KEY`.

### Fase 2: backend gestione
- Tabella `settings` + API `GET/PUT /admin/settings`, `/workspaces/{id}/settings`, `/projects/{id}/settings`, con schema tipizzato e fonte (env/db/default).
- Utenti: elimina, promuovi/revoca admin istanza, reset password, sessioni (elenca/chiudi), ricerca e paginazione.
- Account: cambio password, password dimenticata, sessioni proprie.
- Email: SMTP, inviti con link (anche a chi non ha un account), verifica email.
- Stream e ambienti: metadati, creazione automatica.
- Socket admin locale.
- `/admin/health`: WAL, coda upload, ultimo upload verificato, evento più vecchio non archiviato, stato destinazione.
- Limiti ingest per token/progetto/workspace (429) e quote.
- Purge manuale con conferma digitata (`logs:delete`).
- Compose: passa tutte le variabili al container.

### Fase 3: nuova shell e impostazioni web
IA nuova, switcher workspace, URL con scope, tre aree di impostazioni, Amministrazione, Account. Storage diviso in Archivio + Conservazione; il gergo (watermark, cache, segmenti) sotto "Avanzate".

### Fase 4: log v2
Migrazioni (tags, stream, event_types), ingest e ricerca, tail con filtri sugli attributi, explorer nuovo, catalogo eventi e regole campi nelle impostazioni progetto.

### Fase 5: plugin 0.2
Schede sopra, client condiviso, socket, env completo, console con storico.

### Fase 6: avvisi e canali
Procedura in tre passi (cosa guardare, quando scattare, chi avvisare) con modelli pronti; riassunti in frase; avvisi di sistema (WAL > 80%, archivio in ritardo, destinazione giù); canali email e Telegram; registro consegne per canale.

### Fase 7: dashboard, utilizzo, registro attività
- Registro attività per workspace e istanza (API già pronte).
- Utilizzo: eventi e byte per progetto/stream nel tempo, quota.
- Dashboard: pannelli salvati (conteggio, serie temporale, top valori) costruiti dalle query salvate.

### Fase 8: SDK e FiveM
`tags`, `actor`/`target`, preset del catalogo eventi FiveM, messaggi migliori negli esempi, privacy del nome giocatore, documentazione in-app aggiornata. La risorsa FiveM crea gli stream mancanti se il progetto lo permette.

### Fase 9: sicurezza e rifinitura
MFA con app authenticator (TOTP) e passkey (WebAuthn), entrambe opzionali per ogni utente, mai obbligatorie, redazione lato server all'ingest, scadenza e rotazione token, test OTLP con un Collector vero, e2e sui nuovi flussi.

## Ordine e dipendenze

```
F2 backend ─┬─> F5 plugin
            ├─> F1 bug + F0 design ──> F3 shell web ──> F4 log v2 ──> F6 avvisi ──> F7
            └─> F9 (MFA, passkey)            F4 ──> F8 SDK/FiveM
```
Si parte da F2 (scelta 2026-11-04). F2 è divisa in PR:
- F2a impostazioni in DB a tre livelli + compose che passa tutte le variabili.
- F2b gestione utenti e account (elimina, admin, reset password, sessioni, cambio password, ricerca).
- F2c SMTP, inviti con link, password dimenticata, verifica email.
- F2d metadati di stream e ambienti, creazione automatica configurabile.
- F2e socket admin locale + token admin d'istanza + CORS per Bearer.
- F2f `/admin/health`, limiti e quote di ingest, purge.

## Avanzamento al 2026-11-06

F2a–F2f e F1 completate. F0 completata: shell B014, impostazioni B015, utenti B016 e plugin A017
approvati; riferimenti in `docs/DESIGN.md`, prototipi autonomi in `docs/redesign/approved/`,
token allineati in `web/src/index.css`. Build, lint e 67 test web passati.
Prossima fase F3: nuova shell e impostazioni web, poi F4–F9 nell’ordine concordato.
Le preview non sono integrazioni React/API/SDK. Nessun deploy o push in questa sessione.

## Avanzamento implementazione 0.2 (2026-11-06)

F3–F9 implementate nel checkout: shell e gestione per scope, Log v2 con catalogo/redazione,
plugin a nove sezioni e client condiviso, avvisi email/Telegram e di sistema, dashboard/utilizzo,
SDK/FiveM e sicurezza account facoltativa (TOTP, recupero e passkey). Versione plugin preparata 0.2.0.
Dettagli e limiti in `docs/releases/0.2.0.md` e contratti in `docs/redesign/SECURITY-API.md`.

Verificati: 115 test web, lint/typecheck/build, Go race/vet e compilazione dei test integration,
OpenAPI valida, SDK22, plugin15, client condiviso7 e FiveM28. Build Linux amd64/arm64 con web incorporata
e build plugin da sorgenti isolati passate. QA della build web a1440/390 con API fixture senza errori o overflow.

La validazione dello stack resta aperta: Docker non risponde nemmeno a `/_ping` sul socket.
Non eseguiti integration PostgreSQL/ClickHouse/archivio, smoke,13 scenari E2E reali e Collector;
i test sono preparati e i relativi comandi/CI sono aggiornati. Non verificati host Ervisio,
provider OAuth reali, dispositivi passkey fisici e hook in-game. Nessun commit/push/tag/deploy.
