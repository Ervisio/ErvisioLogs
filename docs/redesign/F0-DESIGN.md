# F0 · Design in corso

Stato: shell/explorer B014 e impostazioni B015 approvati il 2026-11-06. Anche utenti B016 e plugin A017 approvati; F0 completa. Fonte della direzione definitiva: ../DESIGN.md.

## Brief

Modalità product-app, desktop e mobile; fidelity existing. Destinazione React 19 + Tailwind 4.
La base resta quella approvata in `ervisio-logs-spec/design/UI_DECISIONS.md`: nero OLED,
ambra, Figtree e JetBrains Mono. Superfici opache sovrapposte, bordi sottili, nessuna texture.
Il redesign esplora gerarchia e densità, senza riaprire l'identità del prodotto.

Pubblico: gestori di server e sviluppatori. La prima lettura deve dire chi ha fatto cosa,
nel progetto e nell'ambiente selezionati. I dati tecnici restano accessibili nel dettaglio.

## Riferimenti

- Prototipi Ervisio già approvati: rail dei progetti, stream come canali, colori e font.
- [Grafana Explore](https://grafana.com/docs/grafana/latest/visualizations/explore/logs-integration/):
  istogramma sopra i risultati, dettaglio inline o laterale, campi che diventano filtri, Live
  nello stesso explorer. Riutilizzati i principi di gerarchia, non la palette.
- Better Stack Live Tail: riferimento previsto; pagina non accessibile durante questa sessione,
  quindi nessuna conclusione visiva ricavata da questa fonte.

## Token del confronto

I token sono definiti prima del markup nel `_shared.css` del round. Si riusano colori di
`web/src/index.css`, con sette colori semantici per i livelli e un bordo più visibile.
Figtree per UI e messaggi leggibili; JetBrains Mono per identificatori, orari e JSON.
Spaziatura su base 4 px; raggio controlli 7 px; superfici distinte senza ombre decorative.
Le font sono servite dal progetto, senza CDN. Tema chiaro incluso nei token.

## Round 007 · Shell ed explorer

File locali: `.variant-studio/rounds/007-shell-explorer-v02/` (gitignored).
Gruppo: `Ervisio Logs / App shell`. Target laptop e mobile.

- A, Canali e server: rail progetti + colonna stream + dettaglio laterale.
- B, Workspace compatto: sidebar unica con progetti e scope, dettaglio laterale.
- C, Log in primo piano: sezioni progetto in alto e dettaglio dentro la lista.

Le anteprime sono statiche e lo dichiarano. Eventi e nomi vengono dal prototipo già approvato;
non rappresentano dati di produzione. Stati selezionabili: default, loading, empty, error,
detail (su mobile mostra il pannello dettaglio). I controlli non chiamano API.

Verifica: lint anti-slop PASS sulle tre varianti; render Playwright a 1440 e 390 px, screenshot
esaminati; controlli a 1280 e 390 px sui cinque stati senza overflow orizzontale.
Il comportamento reale, i permessi e l'accessibilità dei flussi saranno verificati durante F3/F4.

## Dopo la scelta

Applicare feedback e token del round scelto, poi confrontare impostazioni, utenti admin e plugin.
Solo dopo approvazione consolidare `DESIGN.md` e trasferire i token alla web app.

## Feedback e round 008 · Shell pulita

Decisione `b4ad9696cf3b`: l’utente riparte da A e chiede meno elementi visibili sia
nell’area log sia nella sidebar, mantenendo le funzioni. Nessuna approvazione finale.
Il feedback arriva dal tema chiaro; il target resta laptop e mobile.

Il round `008-shell-clean` esplora due revisioni della stessa rail: righe compatte oppure
messaggi su due righe. Filtri, dettaglio e gestione si aprono su richiesta. Niente istogramma
né sette chip livello nello stato iniziale. Le funzioni restano nel perimetro F0/F3/F4.

Round 008 presentato: A «Solo l’essenziale», B «Lettura distesa». Verificati sei stati in
entrambi i temi a 1280 e 390 px, senza overflow e senza errori browser. Screenshot esaminati.
A espone anche lo stato timeline per il grafico su richiesta; B lo raccoglie in un details.
Nessuna modifica React prima della scelta. Le API e i flussi rimangono fuori da questi mock.

## Feedback e round 009 · Ricerca e densità

Decisione `eb128fb48061`: l’utente mantiene A, chiede ricerca più minimale, meno spazio
per ciascun evento e icona crea progetto centrata. La rail identifica il workspace in cima
(logo, immagine o iniziali); click apre la selezione workspace. Sotto compaiono i suoi progetti.
Si usano le iniziali ER per Ervisio, perché non è stata fornita un’immagine workspace.

Round `009-shell-dense`: ricerca in linea oppure comandi nella testata; righe desktop
rispettivamente da 38 e 44 px. Lo stato workspaces mostra la destinazione del logo workspace.
Nessuna nuova approvazione, nessuna modifica React.

Verifica round 009: lint anti-slop PASS; otto stati in entrambi i temi a 1280 e 390 px,
nessun overflow o errore browser. Screenshot desktop/mobile e selezione workspace esaminati.
Densità A misurata: 38 px desktop, 54 px mobile. Logo workspace e + centrati con grid.

## Feedback e round 010 · Materiale e temi

Decisione `c35dc1afc121`: base B, con richiesta esplicita di cambiare stile e palette.
Riferimento Discord, angoli più arrotondati, temi bianco/scuro/AMOLED, prova acrylic/glass
con sfondo astratto. La richiesta riapre i token cromatici precedenti, conservando la gerarchia.

Il confronto usa tre materiali: pannelli pieni, superfici arrotondate separate, acrylic.
I colori arancio derivano come proposta da ErvisioDrive (`../ErvisioDrive/DESIGN.md`),
non da un'identità globale Ervisio approvata. Figtree e JetBrains restano invariati.
Palette chiara con foglio bianco, scura ardesia, AMOLED con area log nero puro.
La selezione dei temi è nel controllo Tema del pannello Regola di Studio.
Sfondo astratto CSS (arancio/ardesia) solo nella prova acrylic; nessuna risorsa remota.

[Riferimento Discord](https://discord.com/blog/improving-mobile-with-squircles-styles-and-spacing/):
server e app squircle, account circolari, superfici distinte e controlli meno affollati.
L'utente richiede il vetro, quindi la precedente esclusione di glass nel brief iniziale non
si applica a questo esperimento. Nessuna scelta finale, nessuna modifica React.

Verifica round 010: tre varianti × due viewport × tre temi × otto stati, senza overflow.
Area main AMOLED misurata `rgb(0, 0, 0)` in tutte le varianti. Screenshot nei tre temi
esaminati; lint anti-slop PASS. Un passaggio ha rilevato `layoutCompare is not defined`
nella galleria; il controllo successivo di galleria e anteprime non lo ha riprodotto.
Il codice della skill non è stato modificato. Nessuna verifica di flussi API, perché i mock
sono statici. Default Tema nel pannello Regola: light; alternative dark e amoled.

## Feedback e round 011 · Acrylic, blu/viola e movimento

Decisione `258810006cc1`: l’utente mantiene C acrylic, cambia l’accento da arancio a blu/viola
e chiede lo sfondo animato. Nessuna approvazione finale. Layout e densità restano quelli di B/009.
La nuova palette è una richiesta esplicita dell’utente, non una deduzione sul brand.

Round `011-acrylic-accent`: due accenti sulla stessa interfaccia, con grandi forme astratte
in movimento lento via transform CSS (30–42 secondi). Il contenuto resta fermo, AMOLED mantiene
il nero nell’area log, prefers-reduced-motion disattiva il movimento del fondo.
Nessun JavaScript o dipendenza nuova. I token del movimento seguono la disciplina transitions-dev;
la durata ambientale è dedicata allo sfondo richiesto, separata dai tempi dei controlli.

Verifica round 011: due varianti × tre viewport (1440/1280/390) × tre temi × otto stati,
nessun overflow dopo il contenimento dei livelli decorativi. Movimento dei due layer verificato
cambiando il tempo delle animazioni; prefers-reduced-motion elimina entrambe. Area AMOLED
nero puro. Screenshot esaminati, lint PASS, nessun errore browser nelle anteprime.

## Feedback e round 012 · Correggere il tema reale della galleria

Decisione `67f0698a5bda`: A blu da rivedere; chiaro respinto, animazione non percepibile.
L'audit segnala accenti e testo secondario sotto contrasto. Errore di verifica del round 011:
i test impostavano data-palette direttamente, mentre il comando della galleria imposta data-theme.
Senza toccare il knob, la classe accent-blue ereditava i colori scuri sopra superfici chiare.

Correzione: light/dark seguono data-theme, data-palette abilita solo AMOLED. Testare gli URL
con ?theme=light e ?theme=dark senza iniettare palette. Chiaro con foglio bianco e accento blu
scuro, testo terziario più scuro; due alternative di opacità del vetro. Movimento più visibile
(12–18 s), mobile 18–24 s. auto rispetta prefers-reduced-motion; on è una scelta esplicita nel
knob di Studio, off ferma il fondo. La nota sul dispositivo compare solo con movimento ridotto.

Verifica round 012: audit nativo di Studio senza issue su entrambe le varianti, URL reali
?theme=light/dark senza data-palette, desktop1280/mobile390. Screenshot esaminati. Movimento
misurato con trascorrere reale di2,5s, entrambe le animazioni avanzano; auto con riduzione ferma,
on esplicito riattiva, off ferma. Nessun overflow a1440/1280/390 nei24 abbinamenti tema/stato.
AMOLED nero puro, lint PASS, nessun errore browser. Tema chiaro ora bianco/blu senza orb scuri.

## Feedback e round 013 · Solo oggetti sfocati

Decisione `c9f69885f057`: base B/012; eliminare il rettangolo visibile del fondo animato.
L’utente vuole solo la sfocatura di piccoli oggetti in movimento. Si rimuovono i grandi
livelli a gradiente e si usano cerchi piccoli con blur statico, animando solo transform.
Il fondo resta neutro; vetro, accento blu, chiaro corretto e densità non cambiano.
Due prove: sei macchie diffuse oppure dieci punti più piccoli. Nessuna approvazione finale.

Verifica round 013: nessun gradiente nell’app o negli oggetti; oggetti rotondi da56–148px,
blur fisso, transform animato. Audit Studio senza issue su light/dark a1280/390; otto stati
senza overflow. Sei/dieci animazioni verificate e movimento ridotto rispettato, on esplicito
funziona. Movimento autonomo misurato in2s e AMOLED nero puro verificato a1440. Screenshot
esaminati, lint PASS, nessun errore browser.

## Feedback e round 014 · Area log più trasparente

Decisione `11ca1941891e`: base A/013, richiesta solo area log leggermente più glassy.
L’opacità si accumulava tra main e lista (92% + 50% ≈96%): rimuovere il fondo aggiuntivo
della lista e usare un solo strato. Due opacità dello stesso vetro, senza cambiare sei oggetti,
layout, palette o densità; AMOLED e fallback di trasparenza ridotta restano opachi.

Verifica round 014: opacità effettive main misurate82%/72% nello scuro e90%/84% nel chiaro;
content/logs/rows trasparenti sul desktop. Audit Studio senza issue a1280/390 in light/dark;
otto stati senza overflow, AMOLED nero puro, mobile opaco conservato. Screenshot esaminati,
lint PASS, nessun errore browser. Oggetti e animazioni identici al round013 verificato.

## Approvazione shell e round015 · Impostazioni

Decisione `b762697cc6a4`: B/014 approvata. Riferimento e token consolidati in `docs/DESIGN.md`
e `docs/redesign/approved/shell/`; prototipo autonomo con font incorporati renderizzato a parte.
Non riaprire palette, layout o sfondo. L’integrazione React non è ancora fatta.

Il round015 usa lo stile approvato per tre organizzazioni delle impostazioni progetto,
sezione Stream: navigazione laterale, schede in alto, selettore con gruppi apribili.
Campi verificati su backend/internal/settings/settings.go (defaulttrue e20) e Meta in store:
Nome visibile/Descrizione/Colore. I quattro stream sono quelli già usati nelle preview.
Nessun dato di produzione o conteggio d’uso inventato, nessuna chiamata API.

Verifica: audit nativo Studio senza issue a1280/390 in light/dark; sei stati senza overflow,
screenshot esaminati, lint PASS, nessun errore browser. Rimossa la riga mobile vuota della
ricerca, perché la testata delle impostazioni contiene solo il contesto.

## Approvazione impostazioni e round016 · Utenti istanza

Decisione `9ee89658eef3`: B/015 approvata. Schede in alto, selettore mobile; riferimento
in `docs/redesign/approved/project-settings/`. Lo stile shell014 resta bloccato.

Il round016 confronta gestione utenti a pannello, inline e scheda affiancata. Contratti F2
verificati in users_handlers.go: ricerca/paginazione, status e is_instance_admin, nuova password,
sessioni, creazione/eliminazione. Email e username non sono modificabili da PATCH; reset admin
imposta una nuova password e chiude sessioni, non manda un link email. L’admin non modifica
stato/ruolo/elimina sé stesso da questa area. Amministrazione non concede accesso ai log tenant.
Account campione Nico dal prototipo approvato, ada e bob dai test web; nessuna attività inventata.

Verifica round016: audit Studio senza issue per otto stati a1280/390 in light/dark,
nessun overflow o errore browser. Screenshot elenco, profilo e creazione mobile esaminati,
lint PASS. Il confronto parte dal dettaglio per mostrare la differenza tra pannello, inline
e scheda affiancata. Le azioni restano statiche e non modificano account reali.

Correzione finale016: password e sessioni dell’account corrente rimandano ad Account;
il backend rifiuta adminResetPassword su sé stessi. Solo gli altri utenti hanno reset admin.
Verifica aggiornata dello stato self a1280/390: audit Studio senza issue.

## Approvazione utenti e round017 · Plugin

Decisione `f99e4d71a68d`: B/016 approvata. Profilo nella riga; riferimento salvato in
`docs/redesign/approved/admin-users/`. Lo stile e le altre scelte restano bloccati.

Il round017 confronta menu laterale, schede e gruppi per le nove sezioni del plugin0.2.
Contesto Ervisio/Plugin, senza duplicare il launcher workspace della web app. Panoramica con
stati dichiaratamente illustrativi, dai campi reali di admin/health e plugin Status; nessuna
versione, attività o metrica di produzione inventata. Letti kit.ts, LogsPage.tsx, Overview.tsx,
logs.css e health_handlers.go. La traduzione F5 usa sdk.ui e le capability già dichiarate;
SDK non montato in queste preview, nessun comando o endpoint chiamato.

Verifica round017: audit Studio senza issue per sei stati a1280/390 in light/dark,
nessun overflow o errore browser. Screenshot desktop/mobile esaminati, lint PASS.
Il pulsante Apri web app sparisce quando servizio/collegamento non sono disponibili.
I valori della panoramica sono dichiarati illustrativi. Il kit SDK reale non è montato.

## Approvazione plugin e chiusura F0

Decisione `42c8e0d15968`: A/017 approvata. Riferimento autonomo in
`docs/redesign/approved/plugin/`; licenze dei font e delle icone conservate.
I quattro riferimenti sono consolidati in docs/DESIGN.md. Token portati in web/src/index.css:
blu, scuro/chiaro/AMOLED, sette livelli, vetro/densità/movimento, raggi. Selettore AMOLED e nuova
struttura React appartengono a F3; SDK/API plugin a F5. Nessun deploy o push.

Verifica codice: lint, 67 test, build TypeScript/Vite passati. Smoke visivo login della build
con endpoint auth simulati a 1440/390 nei tre temi: colori e font corretti, nero AMOLED,
nessun overflow o errore browser. Build emette avviso chunk JS 574 kB; non è un blocco.
