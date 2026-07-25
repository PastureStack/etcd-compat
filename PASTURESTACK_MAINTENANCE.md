# PastureStack Maintenance

PastureStack is an independent community effort to preserve, audit, and modernize the Rancher 1.6 ecosystem. It is not affiliated with or endorsed by Rancher Labs or SUSE.

This GitHub fork keeps the etcd 2.3.7 storage and protocol boundary buildable on maintained toolchains and base images. Git history, authorship, dates, tags, copyright notices, and the Apache-2.0 license remain authoritative for inherited work.

Treat every behavior change as storage- and cluster-sensitive. Validate it against single-member and multi-member fixtures, snapshot restore, upgrade, rollback, and the complete PastureStack Kubernetes infrastructure stack before publication.

The release source scan loads `trivy-secret.yaml`. Its two path-scoped rules cover exactly three public upstream test keys whose content hashes are enforced by `scripts/validate`; these fixtures are never copied into the runtime image and must never be used as credentials. Any added, moved, or modified private key fails validation or the secret scan.
