# Changelog — playfab

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
