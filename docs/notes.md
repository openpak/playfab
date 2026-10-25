# Notes: playfab

Standalone PlayFab-compatible HTTP service. Core Keeper on Switch is the first
supported title: PlayFab `94845`, application `010070201D1BA000`. DOOM +
DOOM II (Switch application `01008CB01E52E000`) is the registered second
title: its tenant is built but stays off until its PlayFab TitleId is
captured — see [the DOOM plan](doom-doom2.md).

See [protocol provenance](provenance.md) and the original
[Core Keeper requirements](../../npln/titles/core-keeper/docs/playfab-requirements.md).

The [Core Keeper protocol flow](core-keeper-flow.md) records the decompiled
login, allocation, descriptor integrity, presence and invitation gates. The
`internal/party` package contains reviewed allocation models and connection
validation plus the region placement rule (`placement.go`); the allocation
endpoint and relay transport are still unimplemented.

License: AGPL-3.0-only.
