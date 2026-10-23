# OpenPak PlayFab

Standalone PlayFab-compatible HTTP service. Core Keeper on Switch is the first
supported title: PlayFab `94845`, application `010070201D1BA000`. DOOM +
DOOM II (Switch application `01008CB01E52E000`) is the registered second
title: its tenant is built but stays off until its PlayFab TitleId is
captured — see [the DOOM plan](docs/doom-doom2.md).

Deployed on 2026-10-12 as `openpak-playfab`, using a locally built arm64 image.
The service, trusted TLS route and restart persistence pass server checks.
Physical Switch login and ticket/entity exchange returned 200 on the first
retest; online entry returned to the menu with an “OK”/A-button prompt.
Multiplayer acceptance and the next Party dependency remain unresolved.
See [deployment evidence](docs/deployment.md).

The service runs separately from both nx-baas and NPLN. nx-baas terminates the
existing console TLS and forwards **only** `94845.playfabapi.com` when
`NX_PLAYFAB_URL` is configured. Unsupported PlayFab titles retain their 503
response. Nintendo credential verification remains in nx-baas.

| API | Behavior |
| --- | --- |
| `POST /Client/LoginWithNintendoServiceAccount` | Proves `IdentityToken` through nx-baas, checks the verified application, creates/loads the local title player, returns a session ticket and title-player entity token |
| `POST /Authentication/GetEntityToken` | Exchanges `X-Authentication` or refreshes `X-EntityToken`, rechecks account locks and linkage, returns the same entity with renewed expiry |
| `POST /MultiplayerServer/ListPartyQosServers` | When enabled, advertises the service's bound UDP beacon in `UkSouth`; otherwise HTTP 503 |
| Other operations, including `/Party/RequestParty` | PlayFab `ServiceUnavailable` envelope, HTTP 503 |

Responses use the SDK's `code`, `status`, `data` envelope. Credentials are
separate HMAC-authenticated local token types with a one-hour lifetime, scoped
to the title, proven account, Nintendo identity and entity. No Nintendo token
is retained. Refresh accepts exactly one authentication header; it cannot
switch entities or spend an entity token as a session ticket.

`CreateAccount=false` never creates a player. Mapping and signing material
are persisted together in a private, atomically replaced `playfab.json`.
Startup refuses corrupt state or a second writer. Keep the entire state
volume across upgrades and restarts: replacing it changes player identifiers
and invalidates credentials. A failed mapping write never reports success.

Requested combined info describes local account/profile data and empty local
collections. This service starts with no character, inventory, statistics,
currency or user-data records; it does not import Microsoft purchases or
friends. New-player profiles are omitted according to the login contract.
Account lock decisions retain nx-baas's existing cache and backend behavior.

Requests have a 32 KiB body limit and are never dumped. Logs contain only the
operation and response status; unsupported SDK operations expose their API
name for the next console observation. Queries and credential/header values
are excluded. A diagnostic observer for `/Event/WriteTelemetryEvents` logs
hashed event names and integer error/result fields after proving the entity
credential; it retains no payloads and still returns 503. The health listener
is separate from the console API.

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

An example Quadlet is in [deploy/openpak-playfab.container](deploy/openpak-playfab.container).
Build its local image before starting it, and set nx-baas's
`NX_PLAYFAB_URL=http://openpak-playfab:22650`. The current signed network
profile, TLS certificate fallback and Core Keeper NPLN deployment already
cover the required HTTP host.

Party QoS is optional. Set `PLAYFAB_QOS_ADDR=:3075` and
`PLAYFAB_QOS_HOST=<reachable IPv4 address>`, publish `3075:3075/udp`, and allow
that UDP port through both host and cloud firewalls. Both settings are required;
startup fails if the socket cannot bind. Discovery uses the bound beacon's
address, rather than a placeholder. The implementation follows Microsoft's
[UDP beacon protocol](https://learn.microsoft.com/en-us/gaming/playfab/multiplayer/servers/using-qos-beacons-to-measure-player-latency-to-azure): it replaces the leading
`ffff` marker with `0000` and preserves the remaining datagram. Malformed and
oversized probes are ignored; responses are size-preserving and rate-limited.
The internal health endpoint reports `qos_probes_served` when enabled.

Region routing (federation) is optional. Set `OPENPAK_ROUTING_URL` (the
account core, e.g. `http://openpak-account:8000`) and `OPENPAK_ROUTING_KEY`.
`ListPartyQosServers` then lists home's `UkSouth` beacon first, followed by
one entry per other region whose node announces a `playfab_qos` beacon (bare
IPv4 or `ip:3075`), named by the region's Azure name. Test nodes are listed
only for test-allow-listed accounts, identified through the request's entity
token (linked Switch BAAS user); without a valid token the caller is not a
tester. With the URL unset, the table unreachable or no nodes, the reply is
exactly the home-only one. `internal/party.Place` holds the allocation rule
for when an allocator exists: the creator's `PreferredRegions` in order, the
first whose own region has a `playfab_party` relay node (or is home) wins,
unknown names are skipped; then the account country; then home (`UkSouth`).
Joiners reuse the network's placement (`party.Networks`). The QoS beacon is
not yet runnable on a node by itself (the binary always serves the API and
needs `NX_INTERNAL_KEY`), and there is no Party relay to run there yet.

Console acceptance remains a separate check: restart Core Keeper, pass “Please
wait”, observe the next API, host a world, then join from another account and
machine. Authentication support alone does not establish multiplayer. Party
networking/relay support remains separate. A live physical-console inspection
on 2026-10-12 found `PlayFabPartyNetworking` selected even with cross-play off.
QoS is implemented and its public UDP response is verified; Party allocation
and relay transport are not implemented, and two-device gameplay is not yet
verified. This service does not advertise relay addresses.

See [protocol provenance](docs/provenance.md) and the original
[Core Keeper requirements](../npln/titles/core-keeper/docs/playfab-requirements.md).

License: AGPL-3.0-only.

The [Core Keeper protocol flow](docs/core-keeper-flow.md) records the decompiled
login, allocation, descriptor integrity, presence and invitation gates. The
`internal/party` package contains reviewed allocation models and connection
validation plus the region placement rule (`placement.go`); the allocation
endpoint and relay transport are still unimplemented.
