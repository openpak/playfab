# Core Keeper PlayFab deployment — 2026-10-12

Installed at 17:56 UTC on the existing OCI host, separately from nx-baas and
Core Keeper's NPLN container. This is a local experiment image, not a published
registry release.

| Component | Deployment |
| --- | --- |
| PlayFab | `openpak-playfab.service`, initial image `localhost/openpak-playfab:20261012`; diagnostic update `localhost/openpak-playfab:observe-20261012` |
| HTTP / health | 22650 / 22651 on `openpak.network`, no host ports published |
| State | Podman volume `openpak-playfab`, mounted at `/app/state:U` |
| Adapter | `openpak-nx-baas.service`, image `localhost/openpak-nx-baas:playfab-20261012` |
| Forward | `NX_PLAYFAB_URL=http://openpak-playfab:22650`, only `94845.playfabapi.com` |
| NPLN | Existing `openpak-core-keeper.service`; not replaced |

The PlayFab experiment image uses the same cached distroless runtime as the
service Dockerfile, with an independently built Go 1.27 arm64 executable.
The adapter image retains the prior `bcat-20261012` runtime and replaces only
its executable. Existing BCAT configuration, Go-Go Town! version exception,
volumes, TLS/DNS and Traefik routing were retained.

Private deployment artifacts, binary checksums, adapter configuration backup
and guarded rollback are on production at
`~/openpak/experiments/playfab-core-keeper-20261012/`. The rollback refuses
concurrent configuration changes, restores the prior nx-baas image/unit and
stops PlayFab; it retains the PlayFab state volume and other services. Local
build/probe artifacts are in `scratch/playfab-core-keeper/`.

## Validation

- Standalone `GOWORK=off go test -race ./...` and `go vet ./...` pass.
- Full nx-baas tests, race checks and vet pass with the forwarding/identity
  changes and the existing uncommitted BCAT work preserved.
- Tests exercise valid SDK login envelopes, `CreateAccount`, separate account
  identities, concurrent first login, stable mapping/key across restart,
  session exchange and entity refresh with renewed expiry. Invalid Nintendo
  tokens, missing/wrong application and unlinked identities are rejected.
- Credential tests reject tampering, expired/future/wrong-title/wrong-entity
  credentials, swapped token types and ambiguous headers. Refresh preserves
  bans, deletion and linkage checks; identity outages fail closed at the
  PlayFab service boundary. Existing nx-baas ban backend behavior is retained.
- Request bounds, malformed JSON, failed persistent writes, corrupt state,
  duplicate writers, credential-free logs and refused internal HTTP redirects
  are covered.
- Live HTTPS probes use the trusted OpenPak CA and force the backend IP with
  `--resolve`, without contacting Microsoft. Results:

| Probe | Result |
| --- | --- |
| Core Keeper login, deliberately invalid IdentityToken | 401 `NotAuthenticated` |
| Core Keeper host with wrong body TitleId | 400 `InvalidTitleId` |
| GetEntityToken without a credential | 401 `NotAuthenticated` |
| ListPartyQosServers | 503 `ServiceUnavailable` |
| Unsupported PlayFab title `3f284` | Existing 503 `ServiceUnavailable` |

PlayFab was restarted at 17:57 UTC. The persistent signing key was unchanged,
the health check passed, and nx-baas, PlayFab and Core Keeper's NPLN services
were all active. There were **zero** local title-player mappings at that
probe, so this is key-persistence evidence, not a successful console login.

## Next device observation

Restart Core Keeper on the physical Switch. Confirm it passes “Please wait”
without “Failed to authenticate”, then record the next PlayFab operation and
response from `openpak-playfab` logs (no credentials). Host a world, check
NPLN GameSession/Gamesync, and join with a second account/machine. Only that
establishes working multiplayer and whether Party networking remains out of
scope. No dummy Party QoS addresses or relays are advertised.

## First physical Switch retest, 18:28–18:30 UTC

- Startup `ListPartyQosServers` returned 503 at 18:28:54.
- NPLN authentication and Friends/Presence succeeded at 18:29:19.
- `LoginWithNintendoServiceAccount` returned 200 at 18:29:54.
- `GetEntityToken` returned 200 at 18:29:55, using an issued credential.
- Party then called `/Event/WriteTelemetryEvents` three times and repeated
  `ListPartyQosServers`; these operations returned 503.
- The owner initially described a failed online start, then clarified that
  there was **no failure text**: only “OK” with an A-button symbol, returning
  to the main menu. Do not record the earlier paraphrase as the popup's text.
- No NPLN GameSession creation or Gamesync request was observed in this test.

This proves console login and ticket/entity exchange are reached successfully;
it does not establish online entry or multiplayer, or isolate the cause of the
subsequent return to the menu. Party initialization, token acceptance and QoS
need further observation; relay creation is not yet proven mandatory.

At 18:35 UTC a bounded Party telemetry observer was installed in the separate
PlayFab service. It verifies the entity credential and current account linkage,
reads at most 32 KiB and 200 events, and logs only hashed event names plus
allowlisted integer error/result fields. Strings, headers, arbitrary payloads
and credentials are neither printed nor retained. It continues returning 503
for telemetry because a telemetry store is not implemented. QoS behavior is
unchanged. Tests and vet pass; health is active. This update prepares the next
device diagnostic; it is not a claimed fix for the return to the menu.

## Party QoS deployment — 22:15–22:28 UTC

The live physical Core Keeper process was inspected with version-guarded,
read-only observations. Its networking manager selected `PlayFabPartyNetworking`;
its primary implementation was null and the cross-play preference was off.
The earlier assumption that Nintendo-only transport was active is not supported
by this run. Party telemetry error 4202 means that the region list could not be
determined; 4266 follows failed network creation, according to Microsoft's
[error reference](https://learn.microsoft.com/en-us/xbox/playfab/multiplayer/networking/reference/partyerrors).

`ListPartyQosServers` now has an optional real beacon implementation. The
separate PlayFab container was updated to
`localhost/openpak-playfab:qos-20261012`. Its UDP listener is published on 3075,
and the SDK envelope advertises `145.241.199.19` in `UkSouth`, corresponding to
the existing London deployment. The beacon follows the documented FF/FF to
00/00 marker replacement and preserves the rest of the datagram. Requests and
source addresses are not logged; packet sizes and rate-tracking memory are
bounded. Race tests, vet and the arm64 build passed. Tests exercise actual UDP
round trips, invalid/oversized packet rejection, discovery and shutdown.

The host firewall rule was installed for UDP 3075. The first public probe timed
out while localhost worked. The owner added the required cloud ingress rule.
At 22:28:26 UTC a public UDP probe received the exact nonce-preserving response
in 24.52 ms from port 3075. HTTPS discovery returned 200 with the advertised
beacon; health reported two served probes. The persistent signing key was
verified unchanged and both existing player mappings were retained. nx-baas
and NPLN were not restarted or modified by this deployment.

Private deployment record, original Quadlet and guarded rollback are in
`~/openpak/experiments/playfab-qos-20261012/` on production.
`rollback-qos.py` restores only the previous PlayFab unit/image and removes its
exact host firewall rule; it retains the persistent state volume. The cloud
ingress rule was added by the owner and is outside that script's scope.

QoS support does not implement `/Party/RequestParty` or a Party relay. A fresh
Switch launch/host and subsequent invitation/Harbor join remain required to
observe the next operation and establish multiplayer compatibility.

## Explicit JSON response framing — 2026-10-13 08:30 UTC

The fresh online-world test returned login HTTP 200 at 08:19:41 UTC, but the
armed success/error callback observers did not fire. Read-only inspection found
`PlayFabUnityHttp` as the selected transport with a pending WWW operation;
`PlayFabWebRequest` was uninitialized. A pending Post coroutine did not resume
during a bounded observation window. This is evidence of an unfinished client
HTTP/coroutine operation, not proof of a particular response-framing defect.
The early startup trace attached to PID 150 and the process exited; the owner
reported a crash. Startup instrumentation was stopped. PID 152 subsequently
booted and remained stable during guarded inspection at the menu and in-world.

JSON responses now serialize before headers and declare exact Content-Length,
including replies larger than net/http's automatic length buffer. A real HTTP
regression check sends a 4 KiB credential-shaped reply through a streaming
reverse proxy and verifies exact length, complete body and no chunked encoding.
Race tests and vet pass. This removes chunked response framing as a dependency;
it is a compatibility change pending physical-game acceptance, not a confirmed
fix for login or multiplayer.

Production image: `localhost/openpak-playfab:framing-20261013`. Only the
PlayFab container was restarted. The persistent signing key remained unchanged
and two player mappings were retained. Public HTTP/1.1 and HTTP/2 QoS requests
returned 200 with Content-Length 114. UDP publication and cloud ingress were
retained. Deployment and guarded rollback artifacts are in
`~/openpak/experiments/playfab-framing-20261013/`. Run `python3
rollback-framing.py` in that directory to restore the QoS image if needed;
the script rejects a concurrently changed Quadlet and retains persistent state.

Fresh retest after explicit framing: login returned HTTP 200 at 08:35:20 UTC,
with no observed subsequent GetEntityToken or allocation request. The console
GDB handshake failed before attachment, so no post-change client state is
claimed. Framing has not been shown to resolve the startup stall. At the owner's
request, further manual console tests stopped and the full offline main/Party
corpus is being produced under `scratch/core-keeper/full-decomp`, including
complete metadata method signatures and field definitions.
