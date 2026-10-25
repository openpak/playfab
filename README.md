# PlayFab

OpenPak's PlayFab Server.

| | |
| --- | --- |
| Networks | Nintendo Switch |
| Protocol | PlayFab client and authentication API (SDK `code`/`status`/`data` envelope) |
| Image | `localhost/openpak-playfab:local` |
| Licence | AGPL-3.0-only |

## Titles

| Game | Network | Title ID | Module |
| --- | --- | --- | --- |
| [Core Keeper](https://openpak.org/titles#t-72356) | Nintendo Switch | `010070201D1BA000` | PlayFab TitleId `94845` |
| [DOOM + DOOM II](https://openpak.org/titles#t-72353) | Nintendo Switch | `01008CB01E52E000` | PlayFab TitleId `—`; optional tenant (`PLAYFAB_DOOM_TITLE`) |

## Networks

| Network | Reached through |
| --- | --- |
| Nintendo Switch | nx-baas forwards `94845.playfabapi.com` when `NX_PLAYFAB_URL` is set |

## What it serves

| Listener | What | Port |
| --- | --- | --- |
| HTTP API | PlayFab-compatible API, reached through nx-baas | TCP 22650 |
| Health | `GET /health` | TCP 22651 |
| Party QoS beacon (optional) | UDP beacon, when `PLAYFAB_QOS_ADDR` is set | UDP 3075 |

## Run

```sh
GOWORK=off go build ./cmd/playfabd
```

| Environment | Default / requirement |
| --- | --- |
| `PLAYFAB_HTTP_ADDR` | `:22650` (internal HTTP) |
| `PLAYFAB_HEALTH_ADDR` | `:22651` (`GET /health`) |
| `PLAYFAB_DOOM_TITLE` | Unset (DOOM tenant off). Set to the captured PlayFab TitleId to serve `<title>.playfabapi.com` on the same listener |
| `PLAYFAB_DOOM_APP` | `01008CB01E52E000` (override only if a regional build differs) |
| `PLAYFAB_STATE_DIR` | `/app/state` (persistent, writable by service user) |
| `NX_INTERNAL_URL` | `http://127.0.0.1:20070` |
| `NX_INTERNAL_KEY` | Required; same key as nx-baas. `OPENPAK_INTERNAL_KEY` is a fallback. |

## Docs

- [Protocol](docs/protocol.md)
- [Evidence](docs/evidence.md)
- [Deploy](docs/deploy.md)
- [Notes](docs/notes.md)
- [Core Keeper protocol flow](docs/core-keeper-flow.md)
- [Deployment record](docs/deployment.md)
- [DOOM + DOOM II plan](docs/doom-doom2.md)
- [Protocol provenance](docs/provenance.md)

## Licence

AGPL-3.0-only. See [LICENSE](LICENSE).
