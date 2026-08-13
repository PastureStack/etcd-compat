# PastureStack Maintenance

PastureStack is an independent community effort to preserve, audit, and modernize the Rancher 1.6 ecosystem. It is not affiliated with or endorsed by Rancher Labs or SUSE.

This GitHub fork keeps the etcd 2.3.7 storage and protocol boundary buildable on maintained toolchains and base images. Git history, authorship, dates, tags, copyright notices, and the Apache-2.0 license remain authoritative for inherited work.

The current PastureStack package candidate is `2.3.8`; `2.3.7` is the preserved upstream engine version. New release and image versions must remain plain numeric semantic versions and must never add a product-name or maintenance-count suffix.

Treat every behavior change as storage- and cluster-sensitive. Validate it against single-member and multi-member fixtures, snapshot restore, upgrade, rollback, and the complete PastureStack Kubernetes infrastructure stack before publication.

The bundled Bolt page header keeps its original 16-byte on-disk layout while using the maintained header representation and bounded slices required by current Go race and pointer checks. The security gate runs the bundled Bolt test suite before the etcd storage and server suites; do not disable those checks or change the serialized layout to make a build pass.

The compatibility client serializes endpoint replacement, protects watch resume revisions, and uses synchronized progress-report interval accessors. These are concurrency backports for current Go race detection; they do not change the wire protocol or persisted data format.

The release source scan loads `trivy-secret.yaml`. Its two path-scoped rules cover exactly three public upstream test keys whose content hashes are enforced by `scripts/validate`; these fixtures are never copied into the runtime image and must never be used as credentials. Any added, moved, or modified private key fails validation or the secret scan.

The build-only images retain both raw and applicable vulnerability reports. The reviewed OpenVEX document covers only the exact `linux-libc-dev` CVE and package-PURL set found in the compile and race-test toolchain; it cannot suppress a new package, version, vulnerability, secret, or any finding in the runtime candidate.

The `2.3.8` boundary also validates peer bootstrap URLs before the fixed `/members` request, permits environment-supplied `etcdctl` TLS paths only when they exactly match the managed `/etc/etcd/ssl/ca.pem`, `/etc/etcd/ssl/cert.pem`, and `/etc/etcd/ssl/key.pem` files, prevents metric response content sniffing, omits request-controlled and credential-bearing values from logs, and bounds legacy protobuf, procfs, bcrypt, gRPC, and TTL integer conversions. Keep both legitimate and malicious regression cases in the release gate; these controls must not be replaced with scanner exclusions or alert dismissals.
