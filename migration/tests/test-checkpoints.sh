#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
lock_file="${repo_root}/migration/checkpoints.lock.tsv"

test -f "${lock_file}"

expected_versions=(
    3.0.17
    3.1.20
    3.2.32
    3.3.27
    3.4.45
    3.5.33
    3.6.14
    3.7.1
)

mapfile -t rows < <(tail -n +2 "${lock_file}")
if [ "${#rows[@]}" -ne "${#expected_versions[@]}" ]; then
    echo "ETCD_MIGRATION_CHECKPOINT_COUNT_INVALID actual=${#rows[@]}" >&2
    exit 1
fi

for index in "${!rows[@]}"; do
    IFS=$'\t' read -r version checksum purpose extra <<<"${rows[$index]}"
    purpose=${purpose%$'\r'}
    extra=${extra%$'\r'}
    if [ "${version}" != "${expected_versions[$index]}" ]; then
        echo "ETCD_MIGRATION_CHECKPOINT_ORDER_INVALID index=${index} version=${version}" >&2
        exit 1
    fi
    if ! [[ "${checksum}" =~ ^[0-9a-f]{64}$ ]]; then
        echo "ETCD_MIGRATION_CHECKSUM_INVALID version=${version}" >&2
        exit 1
    fi
    if [[ ! "${purpose}" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] || [ -n "${extra:-}" ]; then
        echo "ETCD_MIGRATION_PURPOSE_INVALID version=${version}" >&2
        exit 1
    fi
done

last_index=$((${#expected_versions[@]} - 1))
if [ "${expected_versions[$last_index]}" != "3.7.1" ]; then
    echo "ETCD_MIGRATION_TARGET_INVALID" >&2
    exit 1
fi

echo "ETCD_MIGRATION_CHECKPOINTS_OK count=${#rows[@]} target=${expected_versions[$last_index]}"
