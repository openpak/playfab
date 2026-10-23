# DOOM + DOOM II (2024, KEX) on OpenPak

> **Current Switch client (2026-10-13): Harbor**, at
> `~/REPOS/Openpak/emulators/harbor`. Harbor is an independent
> OpenPak project based on Eden. Citron, Ryujinx (desktop and Android), and
> the former OpenPak Eden checkouts have been removed. References to those
> forks below record historical evidence only: do not run their commands,
> reinstall them, or select them for current work. Use Harbor's README and
> current handoff for builds and testing; old results do not establish Harbor parity.

Target: Switch, online multiplayer first. The 2024 Nightdive KEX re-release
(Switch application `01008CB01E52E000`, eShop nsuId `70010000078053`) plays
16-player crossplay co-op/versus rooms (Switch, PS4/PS5, Xbox One/Series, PC)
over host-authoritative P2P, with matchmaking and identity on **PlayFab
SDK v1.7.2** and a Bethesda.net login required for online play and mod
downloads. Single-player login bypass and the mod browser are out of scope
until multiplayer works.

Status: **not served yet.** `playfabd` carries the title as an optional
second tenant (this document's capture fills in its TitleId); no console has
exercised it.

## What is known

| Fact | Source |
| --- | --- |
| Multiplayer middleware is PlayFab v1.7.2, KEX build 4.3 | PCGamingWiki, DOOM + DOOM II page |
| Steam store requires a PlayFab account + persistent connection | Steam App 2280 page |
| Switch application `01008CB01E52E000` | Blawar/titledb `US.en.json` |
| Older 2019 ports are separate titles (`010018900DD00000`, `0100D4F00DD02000`) | Blawar/titledb |
| Mod browser API base is `https://api.bethesda.net` (`/ugcmods/v1/...`); login/SSO on `bethesda.net/api/attunement/v1/...`; mod pages at `mods.bethesda.net/en/doom_doomii/details/...` | `mods.bethesda.net` SPA bundles, read as public web sources |
| WAD sanity check blocks online play with modified WADs | PCGamingWiki, citing Doomworld |

PlayFab role (unconfirmed until capture): Core auth/discovery plus Party
for NAT traversal/relay and voice — no evidence of Matchmaking queues or
dedicated GameServers. Expect outbound TCP 443 to `<title>.playfabapi.com`
plus UDP hole-punch/relay, like Core Keeper's Party usage.

## Capturing the missing TitleId

The PlayFab TitleId (`<title>.playfabapi.com`, 4–5 hex chars like Core
Keeper's `94845`) is not published anywhere. Do not guess it. Any one of:

1. PC Steam: `strings` on the install binaries/DLLs for `playfabapi.com`.
2. Switch: dump the NSP/EXEFS (`hactool`), `strings` for `playfabapi.com`.
3. Runtime: point a Switch (or Harbor/Ryujinx with OpenPak DNS) at OpenPak,
   open multiplayer, and read the TLS SNI / DNS name for `*.playfabapi.com`
   plus the `TitleId` field in the login POST (nx-baas logs the operation,
   never the body).

Record the TitleId, the exact host, and the first three operations after
login in `docs/provenance.md` before enabling the title.

## Wiring checklist (once captured)

1. `playfabd`: set `PLAYFAB_DOOM_TITLE=<title>` (`PLAYFAB_DOOM_APP` only if a
   regional build differs from `01008CB01E52E000`), restart, confirm the
   startup log names both titles.
2. `nx-baas`: extend the PlayFab forward allowlist in `main.go` (today only
   `94845.playfabapi.com`) with the DOOM host, matching
   `playfab_test.go`'s "unsupported title" cases.
3. Traefik: no new route — `*.playfabapi.com` already terminates at nx-baas,
   which forwards to `openpak-playfab:22650`. No new port: DOOM shares
   22650/22651 with Core Keeper.
4. Console: Switch boots to multiplayer, logs in (expect 200 on
   `LoginWithNintendoServiceAccount` + `GetEntityToken`), hosts a room, a
   second machine joins. Login alone does not establish multiplayer —
   watch for `/Lobby`, `/Match`, `/Party` operations in the `!! unhandled`
   log and implement them in title order.
5. Connectivity: provision a `playfab` backend token in the connectivity
   service config (private `config.json`, never in git), then register
   participant leases per player per room as decoded in step 4. See
   "Shared connectivity integration" below.
5. Bethesda mod hosts (`api.bethesda.net`, `mods.bethesda.net`) stay
   unredirected until multiplayer works; the title's content delivery
   works against the real service.

Clean room: public docs, our own captures, and compatibly licensed code
only. Captured payloads are fact sources, never shipped files.

## Shared connectivity integration

KEX rooms are host-authoritative P2P for up to 16 players, so several players
behind one public IP (one household, CGNAT) must stay distinct participants.
`playfabd` must not grow its own NAT implementation: it becomes a backend of
[`openpak/connectivity`](../../../../../platforms/core/infra/connectivity/README.md) (internal API
`127.0.0.1:20150`, bearer-token authenticated), following the cross-family
plan in [`docs/shared-connectivity-plan.md`](../../../docs/shared-connectivity-plan.md).

- Namespace `playfab`, title `doom-doom2`: one participant lease per player
  per room, keyed by PlayFabID + connection generation — never by IP. Two
  Switches behind one router are two participants with their own candidate
  sets (LAN, reported-public, observed).
- TURN via `/v1/ice` only if the captured transport speaks ICE/TURN (PlayFab
  Party clients may); never advertise TURN to a client that was not observed
  using it.
- Native UDP relay (`/v1/relays`, verified-endpoint bind) only once the
  room-join handshake is decoded far enough to prove each side's endpoint
  toward the relay — destination-specific proof, never IP-learned admission.
  Unsupported stock-client transports stay unsupported rather than falling
  back to guessed mappings.
- Sequencing: login/entity first, then room create/join + P2P handshake
  capture, then connectivity wiring against the measured transport. The
  household acceptance gate is two and three players behind one router
  (same and different local ports, hairpin on/off) holding gameplay, per the
  plan's family checklist.
