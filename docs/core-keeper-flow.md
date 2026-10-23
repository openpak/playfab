# Core Keeper protocol flow and remaining implementation

Research: 2026-10-13. Physical Switch title `010070201D1BA000`, update
1.3.0.3/content 2424832, PlayFab title `94845`. Addresses below are offsets
in the privately inspected main image or Party NRO, not runtime addresses.
No proprietary implementation is reproduced here. Static requirements and
observed execution are distinguished explicitly.

## Ordered flow

1. `NintendoPlayFabAuthentication.Login` (`main+0xf3895a0`, state machine
   `0xf389fd0`) requests a Nintendo service-account token and calls
   `POST /Client/LoginWithNintendoServiceAccount`. It retries failed/null
   results up to five times. The login success callback is `0xf389ec0`;
   error callback `0xf389f20`; request dispatcher `0xf389cf0`.
2. Successful parsing/completion yields `LoginResult`. The game reads
   `PlayFabId` at result offset `0x50` and `EntityToken.Entity` through
   `0x28 -> 0x10`, stores both, then marks authentication complete.
   Receiving HTTP 200 at the server does not establish completion of this
   asynchronous step on the client.
3. Hosting calls `PlayFabPartyNetworking.StartSession` (`0xf3985f0`), then
   `PlayFabPartySessionWrapper.CreateSession` (`0xf3a81e0`). Its state machine
   `0xf3b2b70` waits for any stop operation, sets IsHost/IsInSession, and awaits
   authentication. Those two booleans are set **before** authentication and
   network creation have succeeded.
4. The wrapper joins/creates a Party network, using
   `PlayFabMultiplayerManager.CreateAndJoinNetworkImplStart` (`0xe5113a0`)
   and `_CreateLocalUser` (`0xe5153c0`). SDK operations use the local entity
   identifier and entity credential. Party also uses
   `/Authentication/GetEntityToken` and
   `/MultiplayerServer/ListPartyQosServers`.
5. Native Party posts `/Party/RequestParty` with `X-EntityToken`, validates the
   allocation response, connects to the returned relay, and creates the
   endpoint/authenticated network. Allocation JSON alone cannot complete this.
6. The game obtains the Party join information and calls
   `SwitchPlayFabPartySessionManager.StartSessionAsync` (`0xf40da30`, state
   machine `0xf410a40`). It checks Nintendo network participation and the
   native user context; session data is retained in current-session offset
   `0x18`. The hosting flow then calls `UpdatePresenceForSession`
   (`0xf40e780`). The selected Party path must not be diagnosed solely from
   an absence of NPLN GameSession RPCs.
7. Invite dispatch is `PlayFabPartyNetworking.SendSessionInvitations`
   (`0xf39d8a0`). It can inline the platform send implementation. It rejects
   a missing current session or missing session data before invoking the
   Nintendo friend-invitation API. The eventual callback `0xf40fde0` uses
   import thunk `0xcff727c` to submit native friend invitations. Observe that
   call/result before diagnosing any invitation HTTP handler.
8. Discovery uses `SwitchPlatform.RefreshJoinableSessions` (`0xf3f58e0`) and
   friend/user context/presence. Correctly populated host session information
   is needed before Harbor can discover or join the game.

## Observed call interfaces

These summarize the register arguments relevant to this diagnosis; they are
not complete reconstructed source declarations. Managed methods also carry
IL2CPP method metadata and can be inlined.

| Entry | Relevant arguments / result |
| --- | --- |
| Nintendo login success callback, main+0xf389ec0 | x0 closure, x1 LoginResult; completes the awaited result |
| Nintendo login error callback, main+0xf389f20 | x0 closure, x1 PlayFabError; completes with a null login result |
| Party invite dispatch, main+0xf39d8a0 | x0 networking object, x1 selected friend array, x2 callback |
| Platform session StartSessionAsync, main+0xf40da30 | x0 platform session manager; returns asynchronous completion, rather than a synchronous network-success boolean |
| RequestParty dispatch, Party+0x2772ac | request model, success/error callbacks and callback context; endpoint/header selected internally |
| Allocation response model parser, Party+0x2724dc | x0 response model, x1 parsed JSON data |
| Relay port model parser, Party+0x273874 | x0 port model, x1 parsed JSON port object |
| Descriptor validator, Party+0x386d40 | x0 opaque descriptor, x1 allow-placeholder flag, x2 UUID output, x3 connection-info output, x4 placeholder output; returns Party error code |
| Descriptor finalizer, Party+0x3869f0 | x0 connection information, x1 opaque descriptor; writes relay information and CRC |

The guard used for runtime inspection compares expected instruction bytes
and verifies the Core Keeper program ID before reading these objects. A
hardware breakpoint that has not been independently validated is insufficient
evidence of nonexecution; the corrected software invite observer supplied the
actual selected-friend/session result.

## Login reply

The deployed server emits the standard JSON envelope:

```json
{
  "code": 200,
  "status": "OK",
  "data": {
    "PlayFabId": "<stable account mapping>",
    "SessionTicket": "<issued session credential>",
    "EntityToken": {
      "Entity": {"Id": "<stable entity mapping>", "Type": "title_player_account"},
      "EntityToken": "<issued entity credential>",
      "TokenExpiration": "<actual UTC expiration>"
    },
    "NewlyCreated": false,
    "LastLoginTime": "<previous login time>",
    "SettingsForUser": {"GatherDeviceInfo": false, "GatherFocusInfo": false, "NeedsAttribution": false}
  }
}
```

Additional requested combined information is handled separately. Never use the
literal placeholders above as credentials or account data. The native managed
HTTP response parser checks envelope code 200 and deserializes its data before
queuing the game callback. The public
[Unity SDK transport](https://github.com/PlayFab/UnitySDK/blob/master/ExampleTestProject/Assets/PlayFabSDK/Shared/Internal/PlayFabHttp/PlayFabWebRequest.cs)
likewise separates HTTP receipt, JSON processing and main-thread callbacks.
The inspected native managed parser offsets are `0xe5c8c60` (WebRequest) and
`0xe5c2f20` (Unity transport). Current static analysis has not established a
specific malformed field in the deployed login reply.

## Native Party allocation contract

`Party.nro+0x2772ac` dispatches `/Party/RequestParty` and adds `X-EntityToken`.
The request model's parser/serializer are `0x300080` / `0x300358`.

| Request field | Observed model shape |
| --- | --- |
| BuildAliasParams | Optional object with AliasId string |
| CustomTags | String map |
| PartyId | String |
| PreferredRegions | Array of strings |
| SessionCookie | String |
| Version | String |

Response-model parsing is `0x2724dc`. It recognizes BuildId, ConnectedPlayers,
DTLSCertificateSHA2Thumbprint, FQDN, IPV4Address, LastStateTransitionTime,
PartyId, Ports, Region, ServerId, State and VmId. ConnectedPlayers/Ports are
arrays. Each port has Name, Num and Protocol (`0x273874`); Protocol accepts
string enum values (TCP/UDP), not an assumed numeric wire representation.

The completion/validation path starts at `0x30e2f0`. Its demonstrated
connection constraints are:

- Region must be nonempty and fit a 20-byte buffer including termination.
- FQDN must fit a 256-byte buffer including termination.
- DTLSCertificateSHA2Thumbprint must decode to exactly 32 bytes. The native
  parser accepts hexadecimal digits, with some separator/prefix support;
  an implementation should emit plain 64-character hexadecimal.
- Ports must include exactly one port named `XRNM`, with Protocol `UDP` and
  Num between 1 and 65535. Missing, duplicate, wrong-protocol and out-of-range
  XRNM ports fail. Other named ports are ignored by this connection selection.
- The advertised relay must present the corresponding certificate and
  implement the Party connection/control/data protocol.

These are parsed/validated fields, not proof that returning an invented State,
PartyId or address will create a working network. Allocation has not yet been
observed from the device with the current successful QoS deployment.

## Credentials, descriptors and cryptography

Three separate mechanisms must not be conflated:

- OpenPak login credentials are locally signed opaque credentials. Their
  session/entity prefixes and audiences are distinct; identity validation,
  issuer, subject, title, expiry and linked account are checked by the local
  server. Native Party local-user validation previously inspected accepts an
  opaque bounded entity token; no Microsoft JWT signature check is established
  in that entry path. A JWT expiry routine also exists in the NRO, but its log
  identifies STT token expiration. Its existence does not establish that
  title-player entity credentials must be Microsoft JWTs.
- The Party network descriptor carries a UUID, region, relay address/port and
  certificate fingerprint. Its local integrity field uses CRC32, not a
  demonstrated Microsoft digital signature. `0x386d40` validates the opaque
  descriptor; `0x3869f0` finalizes connection information. The UUID string is
  exactly 36 bytes; region fits 19 bytes plus termination; opaque version is
  2 at offset `0x39`. The CRC covers the UUID bytes, region bytes and 0x128
  connection-information bytes. The stored CRC is at `0x39 + 0x128`. The table begins with
  `00000000, 77073096, ee0e612c, 990951ba`, identifying reflected IEEE CRC32.
  Accumulation is at `0x32f320`, initialized with `0xffffffff` and using the
  reflected CRC table. The inspected native little-endian helpers preserve
  16-bit ports and 32-bit CRC values.
- DTLS authenticates/encrypts the actual transport using the advertised relay
  certificate fingerprint. CRC correctness provides no transport authentication
  and does not replace a relay or its certificate/private key.

Serialized descriptor API `0x3759b0` validates a complete descriptor first.
The serialized representation uses a separate version-3 header and CRC,
including UUID bytes, 20-byte region, 16-bit port and 32-byte fingerprint,
followed by compact hostname material; it is then Base64 encoded.
Deserializer `0x376350` checks size, version, CRC, UUID/region and hostname
reconstruction before rebuilding the opaque descriptor. Do not substitute
Base64 JSON for this SDK-defined binary representation.

## Established failure and implementation status

The corrected physical trace at 2026-10-12 23:07:36 UTC captured invitation
handler entry with one selected friend and no current platform session.
It detached cleanly. Subsequent reads found authentication incomplete, no local
entity key/PlayFab ID, and no local Party user or network handle. Host booleans
were already set. These observations place the immediate failure before
platform session publication and invitation submission; static analysis cannot
by itself prove why that asynchronous login remained incomplete.

Implemented/deployed: verified identity login, stable mappings, signed local
credentials, entity exchange, real QoS API and public UDP beacon.

Still required for complete multiplayer: establish client login completion,
implement real Party allocation and relay transport, then verify platform
presence publication, native invite submission, Harbor discovery and joining.
No Party-compatible relay transport, certificate-backed allocation, or end-to-end success is
claimed. Returning fake allocation success would move the failure into a later
connection stage without fixing multiplayer.

## Shared connectivity dependency

The Party adapter must use [`openpak/connectivity`](../../../../../platforms/core/infra/connectivity/README.md)
for participant leases and compatible relay resources, following the
shared connectivity plan (internal research notes). That
service is already deployed for NPLN, including Core Keeper's running NPLN
container. Its environment points to `http://openpak-connectivity:20150` and a
private token file. Core Keeper reuses the Stardew-family NPLN implementation,
whose `AllocateIceServerSet` handler calls `sharedICE` for Pia ICE resources.
Core Keeper's PlayFab Party path is not yet integrated. These are separate
transport allocation paths: the observed host selects PlayFab Party and stalls
before Party network creation, so existing NPLN ICE integration does not by
itself complete that path. Do not build a second NAT service inside `playfabd`.

- Provision a private backend token for namespace `playfab`. Register participants
  under title `core-keeper`, the authenticated Party session, authenticated player
  identity, a new connection generation on reconnect, and transport component.
  Login alone supplies neither a Party session nor a verified game UDP endpoint;
  do not register guessed room membership from an HTTP source address.
- Preserve each participant separately when several players share a public IP.
  Candidates and relay bindings belong to a participant and generation, never
  to an IP-address lookup. TCP login ports and QoS beacon observations do not
  establish the Party socket's mapping toward a relay.
- Consume the typed connectivity client for lease renewal/release and relay
  allocation. Bind an allocated relay only after the Party adapter verifies each
  endpoint through the native authenticated handshake toward that exact relay.
  Release membership on departure and expire stale generations.
- Keep Party wire decoding, authentication, DTLS certificate handling and native
  network descriptors in the Party adapter. The shared pair relay forwards UDP;
  it does not implement Party's XRNM protocol or DTLS handshake. Its endpoint
  must not be advertised as a working Party allocation until these requirements
  are implemented and verified. Offer TURN only if this client is observed to
  support that exchange.

Acceptance must include physical Switch and Harbor gameplay, two and three
participants behind one public IP, translated ports, reconnect, host reversal,
and attempted cross-session endpoint binding. Shared service tests and successful
QoS probes are prerequisites, not evidence of accepted Core Keeper multiplayer.

## Reviewable code

[`internal/party/allocation.go`](../internal/party/allocation.go) records the
request/connection response models and validates the native relay constraints.
Its tests cover JSON decoding, correct UDP selection, duplicate/missing XRNM
ports, integer overflow, embedded NULs, byte-sized buffers and malformed
fingerprints. This package is a building block: it is not connected to a public
allocation endpoint and cannot create a network by itself.

## Targeted request-completion refinement

The complete metadata map resolves the coroutine's HTTP object to
`UnityEngine.Networking.UnityWebRequest`, rather than the legacy WWW class.
`PlayFabUnityHttp.Update` (`main+0xe5c2690`) is an empty method; its requests
complete through the Post coroutine and Unity's asynchronous request operation.
The coroutine resumes after its yield, checks `UnityWebRequest.error`, and then
calls `OnResponse` or the request-error path. The native request getter
`UnityWebRequest.get_isDone` is `0x116f2380`; AsyncOperation completion is
`0x111914a0`. These provide discriminating observation points for native request
completion versus coroutine resumption, before interpreting game login callbacks.
Static recovery of these gates is not proof that either specific gate failed in
the previous physical run. The previously observed pending request and absent
callbacks establish the incomplete operation; their cause remains unverified.

The native request-result function pointer is cached at `main+0x16c2eeb8`.
The verified `get_isDone` wrapper loads the request native handle from managed
object `+0x10`, calls that pointer, and compares the result with zero. This
allows a guarded observer to locate the actual native completion implementation
without guessing a stripped function address. Observe the existing request's
result and coroutine state before changing server responses again. The send
icall is `UnityWebRequest::BeginWebRequest_Injected(System.IntPtr)`; the result
icall is `UnityWebRequest::get_result_Injected(System.IntPtr)`.
