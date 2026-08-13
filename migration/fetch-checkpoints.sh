#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
lock_file="${repo_root}/migration/checkpoints.lock.tsv"
cache_dir=${ETCD_MIGRATION_CACHE_DIR:-"${repo_root}/migration/cache"}

for command_name in curl sha256sum tar; do
    command -v "${command_name}" >/dev/null 2>&1 || {
        echo "ETCD_MIGRATION_REQUIRED_COMMAND_MISSING command=${command_name}" >&2
        exit 1
    }
done

mkdir -p "${cache_dir}"

while IFS=$'\t' read -r version expected_sha purpose; do
    if [ "${version}" = "version" ]; then
        continue
    fi

    archive="${cache_dir}/etcd-v${version}-linux-amd64.tar.gz"
    extract_dir="${cache_dir}/v${version}"
    url="https://github.com/etcd-io/etcd/releases/download/v${version}/etcd-v${version}-linux-amd64.tar.gz"

    if [ -f "${archive}" ] && ! echo "${expected_sha}  ${archive}" | sha256sum --check --status; then
        rm -f "${archive}"
    fi

    if [ ! -f "${archive}" ]; then
        curl --fail --location --retry 4 --retry-all-errors \
            --connect-timeout 20 --output "${archive}.part" "${url}"
        mv "${archive}.part" "${archive}"
    fi

    echo "${expected_sha}  ${archive}" | sha256sum --check --status || {
        echo "ETCD_MIGRATION_DOWNLOAD_CHECKSUM_MISMATCH version=${version}" >&2
        exit 1
    }

    rm -rf "${extract_dir}"
    mkdir -p "${extract_dir}"
    tar --extract --gzip --file "${archive}" --directory "${extract_dir}" \
        --strip-components=1 "etcd-v${version}-linux-amd64/etcd" \
        "etcd-v${version}-linux-amd64/etcdctl"

    utility_path="etcd-v${version}-linux-amd64/etcdutl"
    if tar --list --gzip --file "${archive}" "${utility_path}" >/dev/null 2>&1; then
        tar --extract --gzip --file "${archive}" --directory "${extract_dir}" \
            --strip-components=1 "${utility_path}"
    fi

    test -x "${extract_dir}/etcd"
    test -x "${extract_dir}/etcdctl"
    case "${version}" in
        3.5.*|3.6.*|3.7.*) test -x "${extract_dir}/etcdutl" ;;
    esac
    echo "ETCD_MIGRATION_CHECKPOINT_READY version=${version} purpose=${purpose}"
done < "${lock_file}"
