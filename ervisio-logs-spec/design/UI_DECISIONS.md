# UI decisions

The owner approved three designs in Variant Studio on 2026-11-02. They replace the earlier choices from rounds 1 to 3.
All three decisions came without notes, annotations or token changes.

| Screen | Approved variant | Decision id | Files |
|---|---|---|---|
| Log explorer | Round 004, variant A, "Canali Ervisio" | `4eb6a2501e33` | [`approved/explorer/`](approved/explorer/) |
| Navigation | Round 005, variant B, "Progetti come server" | `fe5b1a9c45fb` | [`approved/navigation/`](approved/navigation/) |
| Sign-in | Round 006, variant A, "Essenziale" | `3f33c0cd729a` | [`approved/access/`](approved/access/) |

Each folder holds the prototype, the round definition, the decision record (local paths made relative) and the
screenshot the owner approved. Design target: laptop and mobile.

## What was approved

- **Log explorer.** OLED black, amber accent `#ffb547`, Figtree for the interface, monospace only for log content and
  JSON. Streams read as channels (`# economy`).
- **Navigation.** Projects appear as Discord-style circles in an expandable rail. A second column holds the project's
  sections and streams.
- **Sign-in.** Only the form on black, with a small mark. Google, Discord and Cognito sit above a divider, then the
  password form. The prototype also covers the registration, first-run and error states.

## Rules for the implementation

Rebuild these designs with the components and tokens in `web/`. Do not paste the prototypes: their data and actions are
simulated, and nothing in them calls the API.

- Self-host the fonts. The prototypes load Google Fonts, which the CSP and the self-hosted requirement forbid.
- Wire the auth guard and the CSRF header to the real API.
- Provide every string in en and it.
- Measure AA contrast and touch targets on the real pages. The prototypes prove neither.

## Defects to fix

The approved screenshots show these problems. Approval covers the direction, so fix them during implementation.

- Explorer: icons render as empty squares or dots, and the channel `#` is missing. Cause not determined: check whether
  the capture or the variant produced it.
- Explorer: "2 error, 1 f..." is cut off and "Vedi 13 di 214 eventi" wraps.
- Navigation: "Esplora log" wraps in the second column, and the stream unread dots are clipped at the column edge.
- Sign-in: "Ervisio Logs" and "Crea un account" wrap.
- Sign-in: the Cognito logo is unreadable at this size. Its source was not verified, so check it against the official
  asset before shipping.

The Navigation and Sign-in subagents were stopped after approval and did not finish their browser checks.
