# Retirement decision

`etcd-compat` is retired as a deployable PastureStack component.

## Why

- Its defining behavior is the preserved etcd 2.3.7 storage and v2 protocol
  boundary. Replacing that engine in place would remove the compatibility the
  repository exists to provide.
- A bounded search across the other PastureStack repositories and GitHub code
  found no consumer of this repository, its image coordinate, or its package
  candidate.
- PastureStack has a separate `PastureStack/etcd-image` 3.7.2 candidate. Its
  README states that redistribution remains blocked pending license review;
  this retired repository does not provide a deployable replacement.

## Consequences

- Do not build, publish, or deploy a new `etcd-compat` image.
- Do not use the historical release for a new cluster.
- Keep the source and historical release only for audit and recovery planning.
- A remaining v2 data set needs the sequential, offline review process in
  [`migration/README.md`](migration/README.md). A destination runtime and the
  backup, rollback, and rollout plan require separate approval before a live
  migration.

Retirement closes the EOL runtime as an active supply-chain surface without
pretending that an etcd 2.x compatibility engine can be made current by changing
a version label.
