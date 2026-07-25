# Security Policy

## Supported state

The published image is a compatibility candidate for isolated PastureStack integration testing. It is not a general-purpose or currently supported etcd release.

## Review requirements

- Treat dependency and Go toolchain changes as storage-critical changes.
- Run unit, integration, snapshot, WAL, lease, watch, upgrade, rollback, backup, and member-replacement tests against the exact candidate.
- Do not weaken TLS, authentication, peer validation, or request-size controls for compatibility.
- Verify health endpoints against the managed `etcd.<stack>` DNS identity even when the TCP connection targets a container alias or loopback address; never add an insecure TLS fallback.
- Block publication when the exact candidate contains a HIGH or CRITICAL vulnerability or a detected secret.
- Treat the three inherited public test keys as non-deployable fixtures only. Their paths and content hashes are fixed by `trivy-secret.yaml` and `scripts/validate`, and the runtime image must contain none of them.
- Do not commit cluster snapshots, WAL files, credentials, certificates, private endpoints, personal registry coordinates, or production topology.

## Reporting

Report suspected vulnerabilities through this repository's private security advisory channel. Do not include live credentials, private cluster data, or production snapshots in a public issue.
