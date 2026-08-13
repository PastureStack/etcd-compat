# etcd v2 Data Migration Gate

This directory defines a review gate for moving the PastureStack `2.3.8` compatibility package, which preserves the upstream etcd 2.3.7 data boundary, to a currently supported etcd release. It does not upgrade a live cluster, modify a supplied data directory, publish an image, or authorize a production rollout.

## Why this is a separate gate

An etcd 2.3 data directory cannot be opened directly by the current release. Every minor version boundary must be crossed in order. The v2 key store must also be converted with `etcdctl migrate` from etcd 3.4 or earlier because that command was removed in 3.5. Before 3.6, every member must be proven free of custom v2 data in both its snapshot and WAL.

The exact Linux amd64 release archives and SHA-256 checksums are locked in [`checkpoints.lock.tsv`](checkpoints.lock.tsv). The gate currently exercises this path:

`2.3.8 package (2.3.7 engine) -> 3.0.17 -> 3.1.20 -> 3.2.32 -> 3.3.27 -> 3.4.45 -> 3.5.33 -> 3.6.14 -> 3.7.1`

## Read-only production preflight

Run the preflight from a secured administration host against one healthy member. It reads only v2 key metadata and emits counts; it never emits values.

```bash
export ETCD_MIGRATION_CACERT=/secure/path/ca.pem
export ETCD_MIGRATION_CERT=/secure/path/client.pem
export ETCD_MIGRATION_KEY=/secure/path/client-key.pem
bash migration/preflight-v2.sh https://member.example.invalid:2379 preflight.json
```

Client certificate and key variables must be provided together. Certificate verification is never disabled.

The command exits with status `2` when it finds a TTL key. The official offline transformer copies the value but does not preserve its expiry semantics, so every TTL key needs an explicit application-owner decision before migration. Do not bypass this block.

## Isolated verification

`test-v2-to-v3.sh` creates a synthetic three-member cluster under a newly allocated temporary directory. The GitHub gate builds the `2.3.8` package from the checked-out commit and passes that local image to the script; it never substitutes a historical Release image. It:

- extracts the compatibility binaries from the candidate image built from the exact checked-out commit;
- creates deterministic v2 data and proves that the TTL preflight blocks unsafe migration;
- performs every rolling minor-version transition in order;
- requires the restarted member plus a healthy quorum during mixed-version windows, then all three members after each completed minor transition;
- creates v3 data before crossing the 3.2 boundary;
- takes full-cluster copies before and after the v2-to-v3 conversion and restores both copies;
- converts each stopped member with the checksum-locked 3.4 tool;
- removes the migrated custom v2 keys only after their v3 copies are verified on every member;
- forces a current 3.4 snapshot after v2 cleanup so 3.5 does not bootstrap by replaying the original 2.3 version history;
- forces a fresh snapshot and checks every stopped member with `etcdutl check v2store` from 3.5.33;
- disables the v2 API before rolling to 3.6 and 3.7;
- confirms the 3.6 storage version and forces a current 3.6 snapshot before 3.7 removes the legacy v2 loader and changes protobuf internals;
- verifies the complete synthetic data set and three-member health at the target.

The script accepts no live data-directory argument. It is intended for an ephemeral Linux GitHub runner with Docker, not for a production member.

## Production blockers that remain

A passing synthetic gate is necessary but not sufficient. A production plan must still:

1. inventory every control-plane and workload client that calls the v2 API;
2. update those clients to the v3 API before v2 is disabled;
3. collect the read-only preflight from the actual cluster and resolve all TTL keys;
4. take and independently verify a full copy of every member data directory because a v3 snapshot alone does not include v2 data;
5. rehearse the exact topology, certificates, data volume, backup, restore, and rollback in an isolated clone;
6. define a stop point at every minor version and prohibit skipped minor upgrades;
7. keep the existing compatibility image available for rollback until the post-migration observation window closes.

No running container or workload should be recreated merely to migrate the backing key-value store. The control-plane client cutover and the storage migration require a separately approved maintenance procedure.

## Authoritative references

- [Upgrade etcd 2.3 to 3.0](https://etcd.io/docs/v3.3/upgrades/upgrade_3_0/)
- [Sequential etcd upgrade policy](https://etcd.io/docs/v3.4/upgrades/upgrading-etcd/)
- [Migrate v2store data to v3store](https://etcd.io/docs/v3.4/how-to-migrate/)
- [Prepare v2store and WAL for etcd 3.6](https://etcd.io/docs/v3.6/upgrades/upgrade_3_6/)
- [etcd release support policy](https://etcd.io/docs/v3.7/op-guide/versioning/)
