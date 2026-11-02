# Protocol: playfab

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

Core Keeper selects `PlayFabPartyNetworking` even with cross-play off, so Party
allocation and relay transport are required for multiplayer; the service does
not advertise relay addresses.

## Titles

Core Keeper on Switch is the first title: PlayFab `94845`, application `010070201D1BA000`. DOOM +
DOOM II (application `01008CB01E52E000`) is registered second; its tenant is built but stays off
until its PlayFab TitleId is captured ([doom-doom2.md](doom-doom2.md)). The
[Core Keeper flow](core-keeper-flow.md) records the login, allocation, descriptor integrity,
presence and invitation gates. `internal/party` holds reviewed allocation models, connection
validation and the region placement rule (`placement.go`); the allocation endpoint and relay
transport are not implemented. Requirements:
[Core Keeper requirements](../../npln/titles/core-keeper/docs/playfab-requirements.md).

## Contract sources

Written from OpenPak requirements, its internal identity API, public Microsoft contracts and SDK
definitions; no reference backend implementation was used.

- Login route, `TitleId`, `IdentityToken`, `CreateAccount` and result fields follow Microsoft's
  [Nintendo service-account login](https://learn.microsoft.com/en-us/rest/api/playfab/client/authentication/login-with-nintendo-service-account?view=playfab-rest).
- Entity token exchange, UTC expiry, `X-Authentication` / `X-EntityToken` follow
  [GetEntityToken](https://learn.microsoft.com/en-us/rest/api/playfab/authentication/authentication/get-entity-token?view=playfab-rest).
  The optional entity selector from older SDKs can select only the current `title_player_account`;
  title/server secret-key authentication is unsupported.
- Error constants (Unity SDK `PlayFabErrors.cs`): `InvalidParams=1000`, `AccountNotFound=1001`,
  `AccountBanned=1002`, `InvalidTitleId=1004`, `NotAuthenticated=1074`, `ServiceUnavailable=1123`.
- nx-baas's `idToken` carries the application in `nintendo.ai`; `pidForIDToken` verifies
  signature, issuer, type and expiry, and the internal API returns `application_id` only after
  that proof. PID-only refresh checks do not assert an application: the original local credential
  already scopes it.
- Port block 22650-22659 follows Klei ([port plan](https://docs.openpak.org/contributors/ports/)).

## Console observations

- Party's post-login `/Event/WriteTelemetryEvents` requests were seen on a physical Switch; the
  observer reads the documented Events/Payload/PayloadJSON fields, emits hashed names and numeric
  diagnostics, and keeps returning 503 (not a persistence implementation).
- In this build's `Party.nro`, `PartyCreateLocalUser` rejects an entity identifier of 21 bytes or
  more and a token of 8192 bytes or more; registration stores an opaque copy and no JWT decoding
  was seen. OpenPak's identifier and token fit, which argues against a local size/format
  rejection; it does not prove relay token acceptance.
- The managed `PlayFabHasNetworkCheck` callback holds client `GetTitleData`, `GetTitleNews` and
  `GetTitlePublicKey` checks; none appeared in the first post-login window, so they are not the
  immediate failing dependency.
- The successful login and exchange have physical-console 200 observations; online entry and
  subsequent networking were unresolved in the first retest.
- OpenPak's player mapping, token format, persistence and issuer are local decisions and do not
  assert compatibility with Microsoft's private token verification or the native Party relay.
