# Deploy: playfab

`CreateAccount=false` never creates a player. Mapping and signing material
are persisted together in Postgres (tables `playfab_meta`, `playfab_players`); a legacy `playfab.json` is imported once.
Startup refuses a corrupt legacy file. Keep the entire state
volume across upgrades and restarts: replacing it changes player identifiers
and invalidates credentials. A failed mapping write never reports success.

| Environment | Default / requirement |
| --- | --- |
| `PLAYFAB_HTTP_ADDR` | `:22650` (internal HTTP) |
| `PLAYFAB_HEALTH_ADDR` | `:22651` (`GET /health`) |
| `PLAYFAB_DOOM_TITLE` | Unset (DOOM tenant off). Set to the captured PlayFab TitleId to serve `<title>.playfabapi.com` on the same listener |
| `PLAYFAB_DOOM_APP` | `01008CB01E52E000` (override only if a regional build differs) |
| `PLAYFAB_DATABASE_URL` | Postgres URL (required; players and signing key) |
| `PLAYFAB_STATE_DIR` | `/app/state` (legacy `playfab.json`, imported once into Postgres, file kept) |
| `NX_INTERNAL_URL` | `http://127.0.0.1:20070` |
| `NX_INTERNAL_KEY` | Required; same key as nx-baas. `OPENPAK_INTERNAL_KEY` is a fallback. |

Build and check independently of the parent Go workspace:

```sh
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build ./cmd/playfabd
podman build -t localhost/openpak-playfab:local .
```

An example Quadlet is in [deploy/openpak-playfab.container](../deploy/openpak-playfab.container).
Build its local image before starting it, and set nx-baas's
`NX_PLAYFAB_URL=http://openpak-playfab:22650`. The current signed network
profile, TLS certificate fallback and Core Keeper NPLN deployment already
cover the required HTTP host.

## Rewards session events

This host sends none. Rewards sessions (`game_sessions.core-keeper`, `game_sessions.doom-plus-doom-ii`) need
server-observed room membership, and playfabd does not see any: Core Keeper rooms are PlayFab Party networks
that live on the relay, and `/Lobby/*` and `/Match/*` are not implemented here (HTTP 503). Nothing is guessed.
If lobbies are implemented later, carry the nx-baas `baas_user_id` with the entity, keep an `emit.Registry`
(rewards-emit) with `Join`/`Leave`, and set `REWARDS_EVENTS_URL` / `REWARDS_EVENTS_KEY`.

## Account erasure

`DELETE /internal/accounts/{account_id}/data` on the health listener (`http://openpak-playfab:22651`, not routed by Traefik), header `Authorization: Bearer <ACCOUNT_ERASE_KEY>`; key unset answers 503. Answers `{"service":"playfab","rows":N}`.

`playfab_players.account_id` is the OpenPak account id, so no core lookup is needed. Erased: every `playfab_players` row of the account (all titles). Already issued credentials are HMAC-signed and checked against that row on use, so they stop working. Kept: nothing; the token signing key (`playfab_meta`) is the service's, and Party placements are keyed by network id, not account. Server-side PlayFab inventory, stats and friends are not stored by this service.
