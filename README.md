# PastureStack etcd compatibility archive

> [!CAUTION]
> This repository preserves the retired etcd 2.3 data-migration boundary. It
> is not an active PastureStack runtime and must not be used for a new
> deployment. See [RETIREMENT.md](RETIREMENT.md) and the
> [etcd v2 data-migration gate](migration/README.md) before handling existing
> data.

This GitHub fork preserves the etcd 2.3.7 storage and protocol boundary for
historical migration analysis. The abandoned `2.3.8` package candidate will
not be published, and there will be no future `etcd-compat` release. The
upstream engine is end of life and is not a supported etcd release.

PastureStack is an independent community effort to preserve, audit, and
modernize the Rancher 1.6 ecosystem. It is not affiliated with or endorsed
by Rancher Labs or SUSE.

**Upstream:** [`rancher/etcd`](https://github.com/rancher/etcd), in the public
etcd fork network. This fork preserves upstream Git history, authorship,
dates, tags, and license notices. PastureStack maintenance is consolidated
into one commit after upstream version `v2.3.7`.

For the migration boundary and its limits, see [RETIREMENT.md](RETIREMENT.md),
[COMPATIBILITY.md](COMPATIBILITY.md), and [migration/README.md](migration/README.md).
For source provenance and maintenance records, see [ORIGIN.md](ORIGIN.md),
[PASTURESTACK_MAINTENANCE.md](PASTURESTACK_MAINTENANCE.md), and
[SECURITY.md](SECURITY.md). The original upstream README remains available in
the preserved Git history; its installation examples are historical and do
not describe a supported PastureStack deployment.
