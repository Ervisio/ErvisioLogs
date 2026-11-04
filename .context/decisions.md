# Decisions (date · decision · why)
2026-11-04 · Redesign 0.2: piano in docs/redesign/PLAN.md (F0-F9) · audit completo di spec, web, plugin, log.
2026-11-04 · Plugin gestisce tutto con 3 canali: socket admin locale (install locale), token admin d'istanza via HTTPS (istanza remota), file env + riavvio (solo infrastruttura) · scelta dell'utente.
2026-11-04 · Stream auto-create configurabile per progetto dall'admin di progetto: on/off + limite opzionale (anche illimitato) · scelta dell'utente.
2026-11-04 · Ordine: si parte da F2 backend · scelta dell'utente.
2026-11-04 · Perimetro 0.2: SMTP+inviti+reset password, dashboard, canali Telegram/email, MFA TOTP e passkey opzionali (mai obbligatorie) · scelta dell'utente.
2026-11-04 · Settings (ADR 0018): tabella `settings`, precedenza env (bloccata) > progetto > workspace > istanza > default; solo auto_create_streams e max_streams sotto l'istanza · l'env resta l'override dell'operatore, il resto si cambia da UI/plugin senza riavvio.
2026-11-04 · Auto-create stream default on, max_streams default 20 (0 = illimitato), il limite vale anche per la creazione manuale · sblocca gli stream FiveM senza nomi infiniti; limite soft sotto concorrenza.
2026-11-04 · compose.yaml passa le chiavi impostabili come `${VAR:-}` · una variabile valorizzata blocca il setting, quindi niente default nel compose.
2026-11-04 · Rinviati: provider OAuth e metrics token restano solo env (segreti), chiave log level = server.log_level.
2026-11-04 · F2b: l'istanza tiene sempre un admin attivo e ogni workspace un owner (409); un admin non modifica/elimina/resetta sé stesso da /admin (422); cambio password richiede quella attuale e chiude le altre sessioni, reset admin le chiude tutte · evita lock-out e sessioni rubate che sopravvivono.
2026-11-04 · F2c email: link con token nel fragment e consumati con DELETE...RETURNING; forgot risponde sempre 202; inviti anche a chi non ha account, URL mostrato all'invitante solo se non inviato per email (o invio fallito); accettare un link arrivato per email verifica l'indirizzo, uno condiviso no; reset password verifica l'indirizzo e chiude tutte le sessioni · niente enumerazione account, niente token nei log, verifica solo con prova di possesso della casella.
2026-11-04 · F2e (ADR 0019): socket admin locale + token admin `evl_adm_` come "operatori" non utenti, owner senza accesso ai log in ogni workspace · il plugin può gestire l'istanza senza poter leggere i log; rischio accettato: può cancellare workspace o aggiungere utenti propri (auditato).
2026-11-04 · F2f (ADR 0020): limiti/quote ingest via settings (default 0 = illimitato), contatori in memoria per processo; purge asincrona via archiver, rifiutata se taglia segmenti archiviati · Parquet immutabili, niente stato condiviso nuovo per i limiti.
2026-11-04 · Worktree degli agenti partono da main: per lavoro su branch non mergiati, dire all'agente di fare reset/rebase sul branch giusto all'inizio.
