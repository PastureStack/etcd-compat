PastureStack etcd runtime compatibility scripts
================================================

This directory carries the `run.sh`, `delete`, and `disaster` scripts from the
Apache-2.0 upstream etcd image family so the maintained build no longer depends
on a historical runtime image during image construction.

The scripts retain the metadata, giddyup, certificate-action, data-layout,
health-check, backup, and disaster-recovery contracts required by the preserved
control platform. Treat future changes as wire-compatibility changes and
validate them against a Kubernetes infrastructure stack before publishing a
replacement image.
