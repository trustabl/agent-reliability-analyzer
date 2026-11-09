# Source: original fixture

Authored for the trustabl test corpus. Synthetic repo for AGSRV-001 and AGSRV-002.
The root has a Spring application.properties that sets adk.web.cors.origins to *
(AGSRV-001 fires), a Python file that passes a * origin list to get_fast_api_app
(also AGSRV-001, the finding is attributed to the first path-sorted hit), a compose
file that runs the OpenHands agent server image with --host 0.0.0.0, a short port mapping and no
session API key name (AGSRV-002 fires), and a test file with a * origin list (skipped).
safe/ holds a named origin, a loopback port mapping, a keyed service and a service with no --host (all silent).
All values are placeholders. No external code. Public-domain.
