# Evidence: playfab

Deployed on 2026-10-12 as `openpak-playfab`, using a locally built arm64 image.
The service, trusted TLS route and restart persistence pass server checks.
Physical Switch login and ticket/entity exchange returned 200 on the first
retest; online entry returned to the menu with an “OK”/A-button prompt.
Multiplayer acceptance and the next Party dependency remain unresolved.
See [deployment evidence](deployment.md).

Console acceptance remains a separate check: restart Core Keeper, pass “Please
wait”, observe the next API, host a world, then join from another account and
machine. Authentication support alone does not establish multiplayer. Party
networking/relay support remains separate. A live physical-console inspection
on 2026-10-12 found `PlayFabPartyNetworking` selected even with cross-play off.
QoS is implemented and its public UDP response is verified; Party allocation
and relay transport are not implemented, and two-device gameplay is not yet
verified. This service does not advertise relay addresses.
