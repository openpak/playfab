# Changelog — playfab

## v0.4.0 — 2026-11-04

- Account erasure (prds#120): `DELETE /internal/accounts/{account_id}/data` on the health listener (22651; `Authorization: Bearer $ACCOUNT_ERASE_KEY`, unset = 503). `playfab_players.account_id` is the OpenPak account id (nx-baas hands it over at login), so the call deletes every title-player row of the account (PlayFab id and entity id, all titles); issued session credentials stop validating because their row is gone. Idempotent, one `account erased` log line. The signing key and Party placements (keyed by network id, in memory) hold no account data.

## v0.3.0 — 2026-11-03
- Per-request logging: one slog line per HTTP request (method, path, status, ms) on stderr via internal/httplog. Query strings, headers and bodies are never logged; /healthz and /metrics are skipped.

## v0.2.0 — 2026-11-03

**Players and the signing key live in Postgres.** `playfab.json` is replaced by the tables `playfab_meta` (the token
signing key) and `playfab_players` (title-player mappings), small pool (3 connections), so one database backup covers
the service.

- New `PLAYFAB_DATABASE_URL` (or `-database-url`). Without it the service refuses to start; nothing is kept in memory
  or on disk. The state file lock is gone.
- `PLAYFAB_STATE_DIR` (`-state`) now only names a legacy directory: at start a `playfab.json` found there is imported
  once, signing key value identical and players with their timestamps (idempotent, `ON CONFLICT DO NOTHING`, an existing
  database key is never replaced). The file is never deleted or modified. A corrupt file is refused, never regenerated over.
- Restart test (same key, same players), import test, failed-write test (`PLAYFAB_TEST_DATABASE_URL`, skipped when unset).
