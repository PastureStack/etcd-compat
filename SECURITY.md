# Security Policy

## Supported state

The published image is a compatibility candidate for isolated PastureStack integration testing. It is not a general-purpose or currently supported etcd release.

## Review requirements

- Treat dependency and Go toolchain changes as storage-critical changes.
- Run unit, integration, snapshot, WAL, lease, watch, upgrade, rollback, backup, and member-replacement tests against the exact candidate.
- Do not weaken TLS, authentication, peer validation, or request-size controls for compatibility.
- Verify health endpoints against the managed `etcd.<stack>` DNS identity even when the TCP connection targets a container alias or loopback address; never add an insecure TLS fallback.
- Block publication when the exact candidate contains a HIGH or CRITICAL vulnerability or a detected secret.
- Keep raw and applicable scan results for build-only images. A `not_affected` OpenVEX statement is accepted only when the raw HIGH/CRITICAL CVE and package-PURL set exactly matches the reviewed statement set; the runtime candidate must remain zero without VEX suppression.
- Treat the three inherited public test keys as non-deployable fixtures only. Their paths and content hashes are fixed by `trivy-secret.yaml` and `scripts/validate`, and the runtime image must contain none of them.
- Do not commit cluster snapshots, WAL files, credentials, certificates, private endpoints, personal registry coordinates, or production topology.
- Block a v2-to-v3 migration when the read-only preflight finds TTL keys. The official offline transformer does not preserve expiry semantics.
- Never skip an etcd minor-version boundary, run the offline migration against a live member data directory, or disable v2 before every member passes the 3.5.33 v2store-and-WAL check.
- Validate every configured peer as a complete HTTP(S) `host:port` authority before requesting the fixed `/members` path. Reject credentials, paths, queries, fragments, control characters, missing ports, and unsupported schemes.
- Environment-provided `ETCDCTL_CA_FILE`, `ETCDCTL_CERT_FILE`, and `ETCDCTL_KEY_FILE` values are runtime-managed inputs and accept only the fixed `ca.pem`, `cert.pem`, and `key.pem` paths below `/etc/etcd/ssl`; symbolic links are rejected. Explicit command-line paths retain the documented standalone operator contract.
- Never log HTTP header values, remote authorities, request-derived errors, or imported key material. Metric responses must use a non-HTML content type and `X-Content-Type-Options: nosniff`.

## Reporting

Report suspected vulnerabilities through this repository's private security advisory channel. Do not include live credentials, private cluster data, or production snapshots in a public issue.
