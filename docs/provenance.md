# PlayFab authentication provenance

Implementation date: 2026-10-12. Written from OpenPak requirements, its
existing internal identity API, public Microsoft contracts and SDK field/error
definitions. No reference backend implementation was used.

- Core Keeper title/application binding, observed Nintendo login, the startup
  QoS failure and native consumption of `PlayFabId` and entity key are recorded
  in [the Core Keeper requirements](../../npln/titles/core-keeper/docs/playfab-requirements.md)
  and [console provenance](../../npln/titles/core-keeper/docs/provenance.md).
- The login route, `TitleId`, `IdentityToken`, `CreateAccount`, successful
  result fields and requested information follow Microsoft's
  [Nintendo service-account login contract](https://learn.microsoft.com/en-us/rest/api/playfab/client/authentication/login-with-nintendo-service-account?view=playfab-rest).
- Entity token exchange, UTC expiry and `X-Authentication` / `X-EntityToken`
  follow Microsoft's [GetEntityToken contract](https://learn.microsoft.com/en-us/rest/api/playfab/authentication/authentication/get-entity-token?view=playfab-rest).
  The optional entity selector sent by older SDKs can select only the current
  `title_player_account`; title/server secret-key authentication is unsupported.
- Numeric errors are protocol constants from the public Unity SDK
  [PlayFabErrors.cs](https://github.com/PlayFab/UnitySDK/blob/master/ExampleTestProject/Assets/PlayFabSDK/Shared/Internal/PlayFabErrors.cs):
  `InvalidParams=1000`, `AccountNotFound=1001`, `AccountBanned=1002`,
  `InvalidTitleId=1004`, `NotAuthenticated=1074`, `ServiceUnavailable=1123`.
- nx-baas's own `idToken` puts the application in `nintendo.ai`; its existing
  `pidForIDToken` verifies signature, issuer, type and expiry. The internal API
  now returns `application_id` only after that proof. Existing `pidLock` and
  linked-account resolution remain in the path. PID-only refresh checks do
  not assert an application: the original local credential already scopes it.
- Party's post-login `/Event/WriteTelemetryEvents` requests were observed on
  the physical Switch at 18:29:55–18:29:57 UTC. The bounded diagnostic observer
  reads the documented Events/Payload/PayloadJSON fields from Microsoft's
  [telemetry contract](https://learn.microsoft.com/en-us/rest/api/playfab/events/play-stream-events/write-telemetry-events?view=playfab-rest),
  verifies entity authentication, emits only hashed names/numeric diagnostics
  and continues returning 503. It is not a telemetry persistence implementation.
- Bounded inspection of this build's `/Plugins/Party.nro` found the exported
  `PartyCreateLocalUser` at `0x378980`, a parameter check at `0x402a08`, and
  token registration at `0x302620`. The entity identifier must be fewer than
  21 bytes and the token fewer than 8192 bytes. Registration stores an opaque
  copy of the token; no JWT decoding was observed in this local creation path.
  OpenPak's current identifier/token fit those limits. This is static evidence
  against a local size/format rejection, not proof of relay token acceptance or
  a successful native local-user creation. The private plugin, bounded analysis
  and method map are under `scratch/core-keeper/`.
- The managed `PlayFabHasNetworkCheck` callback at `0xf3a9090` contains client
  `GetTitleData`, `GetTitleNews` and `GetTitlePublicKey` checks. None were
  observed in the first post-login Switch window, so they are not being treated
  as the immediate failing dependency.
- Port block 22650–22659 is the next backend block after Klei, recorded in
  [the port plan](https://docs.openpak.org/contributors/ports/).

OpenPak's player mapping, token format, persistence and issuer are local
implementation decisions. They do not assert compatibility with Microsoft's
private token verification or native Party relay protocol. The successful
login and exchange now have physical-console 200 observations; online entry
and subsequent networking remain unresolved in the first retest.
