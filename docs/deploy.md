# Deploy: playfab

`CreateAccount=false` never creates a player. Mapping and signing material
are persisted together in a private, atomically replaced `playfab.json`.
Startup refuses corrupt state or a second writer. Keep the entire state
volume across upgrades and restarts: replacing it changes player identifiers
and invalidates credentials. A failed mapping write never reports success.

| Environment | Default / requirement |
| --- | --- |
| `PLAYFAB_HTTP_ADDR` | `:22650` (internal HTTP) |
| `PLAYFAB_HEALTH_ADDR` | `:22651` (`GET /health`) |
| `PLAYFAB_DOOM_TITLE` | Unset (DOOM tenant off). Set to the captured PlayFab TitleId to serve `<title>.playfabapi.com` on the same listener |
| `PLAYFAB_DOOM_APP` | `01008CB01E52E000` (override only if a regional build differs) |
| `PLAYFAB_STATE_DIR` | `/app/state` (persistent, writable by service user) |
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
