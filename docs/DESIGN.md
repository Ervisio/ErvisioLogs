# Ervisio Logs 0.2 · Design

La shell e l’explorer seguono la variante **B, Vetro più aperto**, round 014, approvata il 6 novembre 2026
(decisione `b762697cc6a4`). Anche le impostazioni progetto seguono B/015, approvata (decisione `9ee89658eef3`). Anche gli utenti d’istanza seguono B/016 (decisione `f99e4d71a68d`). Anche il plugin segue A/017 (decisione `42c8e0d15968`): F0 è completata.

## Ambito

Product-app per desktop e mobile, redesign di un prodotto esistente. Destinazione React 19,
TypeScript e Tailwind 4 già presenti in `web/`. La fase F0 produce riferimenti e token;
l’integrazione della shell e degli endpoint appartiene a F3/F4. Nessuno stile globale salvato applicato.

Riferimento approvato: [prototipo autonomo](redesign/approved/shell/prototype.html),
[screenshot](redesign/approved/shell/screenshot.png), [decisione](redesign/approved/shell/decision.json).
Il prototipo include i font, si apre senza Studio e non chiama API. I controlli sono statici;
gli eventi provengono dal precedente prototipo del progetto e lo dichiarano nel footer.

## Direzione

Navigazione ispirata a Discord, superfici acrylic con accento blu e righe compatte.
Il workspace occupa la prima posizione della rail; sotto compaiono i suoi progetti.
L’utente vede prima messaggio e contesto dell’evento. Filtri, gestione e dettaglio si aprono
su richiesta, per mantenere la vista iniziale pulita.

La richiesta esplicita dell’utente ha sostituito l’ambra iniziale con il blu e introdotto
il vetro. Non riaprire palette, struttura o alternative scartate senza nuovo feedback.

## Riferimenti

- Prototipi Ervisio già approvati: dati, progetti come server, stream come canali.
- [Discord](https://discord.com/blog/improving-mobile-with-squircles-styles-and-spacing/):
  server/app in squircle, persone circolari e controllo della densità.
- [Grafana Explore](https://grafana.com/docs/grafana/latest/visualizations/explore/logs-integration/):
  relazione tra ricerca, Live, filtri e dettaglio dei log.
- ErvisioDrive: superfici arrotondate. L’arancio di quel prodotto è stato provato e scartato
  a favore del blu richiesto dall’utente.

## Token

Valori consolidati della direzione approvata, tema scuro predefinito:

```css
:root {
  --accent: #afc2ff;
  --accent-fill: #7798ff;
  --accent-ink: #0c193c;
  --accent-soft: #283650;
  --ambient-ease: ease-in-out;
  --bg: #17181c;
  --content-blur: 28px;
  --content-edge-open: 36%;
  --content-glass-light-open: 84%;
  --content-glass-open: 72%;
  --critical: #ffa5bb;
  --debug: #b9c5dd;
  --err: #ff8796;
  --font-mono: JetBrains,monospace;
  --font-ui: Figtree,system-ui,sans-serif;
  --glass-blur: 24px;
  --glass-edge: #ffffff1c;
  --info: #86cbed;
  --ink: #f1f2f5;
  --ink2: #bfc2cd;
  --ink3: #a0a5b3;
  --light-active-fill: #e4ecff;
  --light-active-ink: #294fae;
  --light-frost-main: #ffffff;
  --light-frost-side: #f8fbff;
  --line: #363941;
  --notice: #cfb9ed;
  --object-blue: #5b83ea;
  --object-blur: 42px;
  --object-cyan: #53a8d4;
  --object-duration: 16s;
  --object-duration-slow: 22s;
  --object-opacity: .42;
  --object-travel: 90px;
  --object-violet: #8d78dc;
  --ok: #65d9aa;
  --panel-bg: #202127;
  --radius: 11px;
  --radius-panel: 18px;
  --radius-row: 9px;
  --radius-server: 15px;
  --rail-bg: #17181c;
  --raised: #373a44;
  --log-row-height: 42px;
  --log-row-height-mobile: 54px;
  --row-line: #32343c;
  --shadow: 0 8px 28px #00000024;
  --sheet-bg: #25262d;
  --sunk: #2b2d35;
  --surface: #202127;
  --trace: #aab2c2;
  --warn: #eed085;
}
```

In produzione una voce compare quando la sua funzione esiste: le preview rappresentano il target 0.2.
Dashboard (F7), catalogo eventi (F4) e MFA (F9) non devono diventare voci provvisorie durante F3.

Le palette complete chiara e AMOLED sono in [tokens.css](redesign/approved/shell/tokens.css).
Il file consolida le variabili effettive della preview e le altezze misurate delle righe;
non importare l’intero CSS sperimentale dei round nell’app.

| Ruolo | Scelta e motivo |
| --- | --- |
| Accento | Blu con fill e testo distinti, leggibili nei due temi |
| Testo | Figtree per messaggi e UI, JetBrains Mono per identificatori e JSON |
| Pannelli | Raggio 18px main, 16px sidebar; comandi 11px, tile progetto 15px |
| Densità | Riga 42px desktop, almeno 54px mobile; controlli touch 44px |
| Vetro | Main 72% scuro, 84% chiaro, blur 28px; lista e content trasparenti |
| AMOLED | Area log nera piena, vetro e movimento solo ai margini |

## Struttura

- Rail 64px, sidebar 216px, main flessibile; padding esterno 8px e gap 8px.
- Il tile workspace mostra immagine/logo o iniziali (ER nel campione). Il click apre la
  selezione workspace; i progetti nella rail appartengono al workspace corrente.
- Sidebar con ruolo, progetto e ingranaggio, Log/Avvisi/Dashboard, selettore stream e Gestisci.
- Gestisci raggruppa impostazioni workspace e amministrazione; docs e account sono nella rail.
- Testata unica con progetto, ricerca discreta, intervallo, filtri e Live.
- Lista con ora relativa e tooltip data completa, livello, titolo, messaggio e stream;
  dettaglio raggruppato Chi/Cosa/Dove/Quando/Tecnico.
- Sotto 720px rail/sidebar diventano navigazione mobile; i messaggi vanno su due righe.
  La preview mobile usa superfici opache. Dettaglio a pannello sul contenuto.

## Impostazioni progetto

Variante B/015 approvata: sezioni come schede sotto il titolo, selettore nativo su mobile.
Lo scope progetto resta visibile nella testata; l’ingranaggio del progetto identifica l’area.
La preview Stream mostra creazione automatica, limite ereditato e metadati dei quattro stream
esistenti; default true e 20 verificati nel backend. Salvataggio, errore, sola lettura ed editor
metadati si selezionano come stati statici in Studio.

Riferimenti: [prototipo](redesign/approved/project-settings/prototype.html),
[screenshot](redesign/approved/project-settings/screenshot.png),
[decisione](redesign/approved/project-settings/decision.json).

## Utenti dell’istanza

Variante B/016 approvata: elenco compatto e profilo espanso nella riga selezionata.
La navigazione passa allo scope Amministrazione/Istanza; nessun progetto resta selezionato.
Email e nome utente sono in sola lettura. Stato e ruolo d’istanza si modificano nel profilo;
i permessi sui log restano quelli dei workspace. Creazione, password e rimozione hanno dialoghi
dedicati. Il reset admin imposta una nuova password e chiude le sessioni; l’account corrente
rimanda ad Account per password/sessioni e non si disabilita o demuove da questa pagina.

Riferimenti: [prototipo](redesign/approved/admin-users/prototype.html),
[screenshot](redesign/approved/admin-users/screenshot.png),
[decisione](redesign/approved/admin-users/decision.json). I dati sono esempi dai file del progetto.

## Plugin Ervisio

Variante A/017 approvata: header con stato e modalità, menu laterale con Panoramica, Istanza,
Workspace, Progetti, Utenti, Archivio, Server, Indirizzo e Console. Su mobile usa un selettore.
La panoramica distingue servizi, coda ingresso e archivio. Runtime e configurazione host hanno
aree separate. Il plugin vive nel pannello Ervisio e non duplica il launcher dei workspace.
La traduzione F5 usa il kit sdk.ui e le capability dichiarate del plugin.

Riferimenti: [prototipo](redesign/approved/plugin/prototype.html),
[screenshot](redesign/approved/plugin/screenshot.png),
[decisione](redesign/approved/plugin/decision.json). Valori illustrativi dichiarati; nessun server collegato.

## Componenti e stati

| Componente | Regole | Stati |
| --- | --- | --- |
| Navigazione | Tile squircle per workspace/progetti, account circolare | default, hover, active, focus |
| Ricerca | Campo nella testata, filtri su richiesta | default, filtri aperti |
| Log | Riga compatta, colore e livello testuale nel dettaglio/tooltip | default, dettaglio, loading, empty, error |
| Dettaglio | Campi leggibili, originale in disclosure | aperto, chiuso |
| Workspace | Schermata dedicata raggiunta dal tile in cima | elenco, creazione da implementare |
| Temi | Chiaro e scuro autonomi, AMOLED esplicito | light, dark, amoled |

I controlli delle preview rappresentano intenzioni visive. Autorizzazioni, focus dei pannelli,
query e salvataggi reali saranno verificati durante la traduzione in React.

## Movimento

Sei piccoli oggetti circolari 90–148px sul fondo, con blur statico 42px e animazione solo
su transform, circa 14–22s con fasi diverse. Nessun grande livello rettangolare a gradiente.
Contenuto, testo e pannelli restano fermi. auto rispetta prefers-reduced-motion; on è una
scelta esplicita della preview; off ferma il fondo. Con trasparenza ridotta usare pannelli opachi.

## Decisioni

Dopo il confronto iniziale l’utente ha chiesto di ridurre elementi sempre visibili,
compattare le righe e usare una testata unica. Ha poi scelto il materiale acrylic,
l’accento blu, piccoli oggetti sfocati indipendenti e un vetro più aperto per l’area log.
La scelta finale della shell è B/014. I dettagli e gli identificativi delle revisioni sono in
[F0-DESIGN.md](redesign/F0-DESIGN.md); non trattare revisioni precedenti come nuove opzioni.

## Regole di estensione

Conservare palette, densità e gerarchia dei raggi. Presentare poche azioni primarie e tenere
le altre nelle loro aree di scope. Dare alle impostazioni una collocazione sola.
Non sovrapporre un fondo opaco alla lista dentro il vetro; non animare testo, blur o gradienti
rettangolari. Non modificare opacità dell’intero contenitore: ridurrebbe anche il contrasto del testo.

## Traduzione e verifiche

La web app usa già font locali equivalenti (Figtree Variable e JetBrains Mono Variable),
Lucide e token CSS. La traduzione deve riusare componenti, i18n en/it, callback, CSRF,
permessi e dati esistenti. Nessuna nuova dipendenza prevista. I token sono ora in `web/src/index.css`; la struttura React e il collegamento delle API restano a F3/F4.
In web, --hue è l’accento testuale e --accent/--accent-fill il riempimento: conservare questa distinzione.
Le palette CSS comprendono AMOLED; selezione e persistenza del terzo tema verranno esposte in F3.

Preview controllate con Playwright a 1280/390 nei due temi e negli stati principali:
nessun overflow, audit Studio senza issue, AMOLED nero puro. Lo sfondo è stato verificato
con avanzamento reale e movimento ridotto. Prototipo autonomo renderizzato separatamente.
Non sono stati testati flussi API, perché queste preview non ne chiamano.

## Aggiornamenti

- 2026-11-06: approvati shell B014, impostazioni B015, utenti B016 e plugin A017; token integrati nella web app.

Verifiche sul codice: `npm run lint`, 67 test Vitest e `npm run build` (include TypeScript) passati.
Smoke visivo della build login con API auth simulate a 1440/390 nei tre temi: font locali,
palette corrette, AMOLED nero, nessun overflow/errore browser. Il terzo tema è stato selezionato
via attributo CSS per questa verifica. Il build segnala un chunk JS di 574 kB; code splitting fuori da F0.
Nessun flusso backend o SDK reale testato in questa fase. Build locale, nessun deploy.

### Implementazione 0.2

La direzione approvata è ora tradotta nella shell React, nei form per scope, nell’explorer virtualizzato,
nei temi persistenti e nelle nove sezioni del plugin. I riferimenti F0 restano la fonte visiva;
i paragrafi sulla sola preview descrivono la validazione della fase F0, non lo stato attuale del codice.
La build è stata controllata a1440/390 con API fixture; verifiche e limiti aggiornati nelle
[note0.2.0](releases/0.2.0.md). Lo stack reale rimane da verificare quando Docker risponde.
