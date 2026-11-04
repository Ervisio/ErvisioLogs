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
