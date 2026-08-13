#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cache_dir=${ETCD_MIGRATION_CACHE_DIR:-"${repo_root}/migration/cache"}
evidence_dir=${ETCD_MIGRATION_EVIDENCE_DIR:-}
compat_image=${ETCD_COMPAT_IMAGE:-pasturestack/etcd-compat:v2.3.8}
tmp_base=${TMPDIR:-/tmp}
work_root=$(mktemp -d "${tmp_base%/}/pasturestack-etcd-migration.XXXXXX")

case "${work_root}" in
    "${tmp_base%/}"/pasturestack-etcd-migration.*) ;;
    *)
        echo "ETCD_MIGRATION_TEMP_DIRECTORY_INVALID" >&2
        exit 1
        ;;
esac

if [ -z "${evidence_dir}" ]; then
    evidence_dir="${work_root}/evidence"
fi

mkdir -p "${work_root}/nodes" "${work_root}/logs" "${work_root}/backups" \
    "${work_root}/compat" "${evidence_dir}"

required_commands=(cp curl docker env grep jq kill mktemp rm sed seq sha256sum sleep tail)
for command_name in "${required_commands[@]}"; do
    command -v "${command_name}" >/dev/null 2>&1 || {
        echo "ETCD_MIGRATION_REQUIRED_COMMAND_MISSING command=${command_name}" >&2
        exit 1
    }
done

client_ports=(0 12379 22379 32379)
peer_ports=(0 12380 22380 32380)
node_pids=("" "" "" "")
node_versions=("" "" "" "")
compat_container=""
cluster_token=pasturestack-etcd-v2-to-v3-gate
cluster_definition="node1=http://127.0.0.1:${peer_ports[1]},node2=http://127.0.0.1:${peer_ports[2]},node3=http://127.0.0.1:${peer_ports[3]}"
all_endpoints="http://127.0.0.1:${client_ports[1]},http://127.0.0.1:${client_ports[2]},http://127.0.0.1:${client_ports[3]}"

binary_for() {
    local version=$1
    if [ "${version}" = "2.3.7" ]; then
        printf '%s\n' "${work_root}/compat/etcd"
    else
        printf '%s\n' "${cache_dir}/v${version}/etcd"
    fi
}

ctl_for() {
    local version=$1
    if [ "${version}" = "2.3.7" ]; then
        printf '%s\n' "${work_root}/compat/etcdctl"
    else
        printf '%s\n' "${cache_dir}/v${version}/etcdctl"
    fi
}

endpoint_for() {
    printf 'http://127.0.0.1:%s\n' "${client_ports[$1]}"
}

stop_node() {
    local index=$1
    local pid=${node_pids[$index]:-}
    local attempt

    if [ -z "${pid}" ]; then
        return
    fi

    if kill -0 "${pid}" >/dev/null 2>&1; then
        kill -TERM "${pid}" >/dev/null 2>&1 || true
        for attempt in $(seq 1 30); do
            if ! kill -0 "${pid}" >/dev/null 2>&1; then
                break
            fi
            sleep 1
        done
        if kill -0 "${pid}" >/dev/null 2>&1; then
            kill -KILL "${pid}" >/dev/null 2>&1 || true
        fi
    fi

    wait "${pid}" >/dev/null 2>&1 || true
    node_pids[$index]=""
}

stop_all() {
    local index
    for index in 1 2 3; do
        stop_node "${index}"
    done
}

copy_failure_logs() {
    local log_file
    for log_file in "${work_root}"/logs/*.log; do
        if [ -f "${log_file}" ]; then
            {
                echo "--- $(basename "${log_file}") ---"
                tail -n 160 "${log_file}"
            } >> "${evidence_dir}/failure.log"
        fi
    done
}

cleanup() {
    local status=$?
    trap - EXIT INT TERM
    set +e
    stop_all
    if [ -n "${compat_container}" ]; then
        docker rm --force "${compat_container}" >/dev/null 2>&1 || true
    fi
    if [ "${status}" -ne 0 ]; then
        printf 'status=failed\n' > "${evidence_dir}/result.env"
        copy_failure_logs
    fi
    if [ "${ETCD_MIGRATION_KEEP_WORK:-0}" != "1" ]; then
        rm -rf -- "${work_root}"
    fi
    exit "${status}"
}
trap cleanup EXIT INT TERM

start_node() {
    local index=$1
    local version=$2
    local enable_v2=$3
    local snapshot_count=$4
    local cluster_state=$5
    local binary
    local data_dir="${work_root}/nodes/node${index}"
    local log_file="${work_root}/logs/node${index}-${version}.log"
    local -a args

    binary=$(binary_for "${version}")
    test -x "${binary}"
    mkdir -p "${data_dir}"

    args=(
        --name "node${index}"
        --data-dir "${data_dir}"
        --listen-client-urls "http://127.0.0.1:${client_ports[$index]}"
        --advertise-client-urls "http://127.0.0.1:${client_ports[$index]}"
        --listen-peer-urls "http://127.0.0.1:${peer_ports[$index]}"
        --initial-advertise-peer-urls "http://127.0.0.1:${peer_ports[$index]}"
        --initial-cluster "${cluster_definition}"
        --initial-cluster-token "${cluster_token}"
        --initial-cluster-state "${cluster_state}"
        --snapshot-count "${snapshot_count}"
    )

    if [ "${enable_v2}" = "true" ]; then
        case "${version}" in
            3.2.*|3.3.*|3.4.*|3.5.*) args+=(--enable-v2=true) ;;
        esac
    fi

    env -u ETCD_MIGRATION_CACHE_DIR -u ETCD_MIGRATION_EVIDENCE_DIR \
        -u ETCD_MIGRATION_KEEP_WORK \
        "${binary}" "${args[@]}" >"${log_file}" 2>&1 &
    node_pids[$index]=$!
    node_versions[$index]="${version}"
}

wait_endpoint() {
    local index=$1
    local endpoint
    local attempt health
    endpoint=$(endpoint_for "${index}")

    for attempt in $(seq 1 120); do
        if [ -n "${node_pids[$index]:-}" ] && ! kill -0 "${node_pids[$index]}" >/dev/null 2>&1; then
            echo "ETCD_MIGRATION_MEMBER_EXITED node=${index} version=${node_versions[$index]}" >&2
            tail -n 120 "${work_root}/logs/node${index}-${node_versions[$index]}.log" >&2 || true
            return 1
        fi
        health=$(curl --fail --silent "${endpoint}/health") || health=""
        if jq -e '(.health == "true") or (.health == true)' <<<"${health}" >/dev/null 2>&1; then
            return
        fi
        sleep 1
    done

    echo "ETCD_MIGRATION_MEMBER_HEALTH_TIMEOUT node=${index} version=${node_versions[$index]}" >&2
    return 1
}

wait_cluster_health() {
    local index
    for index in 1 2 3; do
        wait_endpoint "${index}"
    done
}

wait_quorum_health() {
    local attempt index endpoint health healthy_count

    for attempt in $(seq 1 60); do
        healthy_count=0
        for index in 1 2 3; do
            endpoint=$(endpoint_for "${index}")
            health=$(curl --fail --silent "${endpoint}/health") || health=""
            if jq -e '(.health == "true") or (.health == true)' \
                <<<"${health}" >/dev/null 2>&1; then
                healthy_count=$((healthy_count + 1))
            fi
        done
        if [ "${healthy_count}" -ge 2 ]; then
            return
        fi
        sleep 1
    done

    echo "ETCD_MIGRATION_QUORUM_HEALTH_TIMEOUT healthy=${healthy_count}" >&2
    return 1
}

wait_cluster_version() {
    local version=$1
    local expected="${version%.*}.0"
    local index endpoint actual attempt

    for attempt in $(seq 1 60); do
        for index in 1 2 3; do
            endpoint=$(endpoint_for "${index}")
            actual=$(curl --fail --silent "${endpoint}/version" | jq -r '.etcdcluster // empty') || actual=""
            if [ "${actual}" != "${expected}" ]; then
                break
            fi
        done
        if [ "${index}" -eq 3 ] && [ "${actual}" = "${expected}" ]; then
            return
        fi
        sleep 1
    done

    echo "ETCD_MIGRATION_CLUSTER_VERSION_TIMEOUT expected=${expected} actual=${actual:-unknown}" >&2
    return 1
}

wait_v3_capability() {
    local version=$1
    local ctl index endpoint attempt all_healthy
    ctl=$(ctl_for "${version}")

    for attempt in $(seq 1 60); do
        all_healthy=true
        for index in 1 2 3; do
            endpoint=$(endpoint_for "${index}")
            if ! ETCDCTL_API=3 "${ctl}" --endpoints="${endpoint}" endpoint health \
                >/dev/null 2>&1; then
                all_healthy=false
                break
            fi
        done
        if [ "${all_healthy}" = "true" ]; then
            return
        fi
        sleep 1
    done

    echo "ETCD_MIGRATION_V3_CAPABILITY_TIMEOUT version=${version} node=${index}" >&2
    return 1
}

wait_storage_version() {
    local version=$1
    local expected="${version%.*}.0"
    local index endpoint actual attempt all_current

    for attempt in $(seq 1 60); do
        all_current=true
        for index in 1 2 3; do
            endpoint=$(endpoint_for "${index}")
            actual=$(curl --fail --silent "${endpoint}/version" | jq -r '.storage // empty') || actual=""
            if [ "${actual}" != "${expected}" ]; then
                all_current=false
                break
            fi
        done
        if [ "${all_current}" = "true" ]; then
            return
        fi
        sleep 1
    done

    echo "ETCD_MIGRATION_STORAGE_VERSION_TIMEOUT expected=${expected} actual=${actual:-unknown}" >&2
    return 1
}

start_cluster() {
    local version=$1
    local enable_v2=$2
    local snapshot_count=$3
    local cluster_state=$4
    local index
    for index in 1 2 3; do
        start_node "${index}" "${version}" "${enable_v2}" "${snapshot_count}" "${cluster_state}"
    done
    wait_cluster_health
}

roll_cluster() {
    local version=$1
    local enable_v2=$2
    local snapshot_count=$3
    local index

    for index in 1 2 3; do
        stop_node "${index}"
        start_node "${index}" "${version}" "${enable_v2}" "${snapshot_count}" existing
        wait_endpoint "${index}"
        wait_quorum_health
    done
    wait_cluster_health
    wait_cluster_version "${version}"
    wait_v3_capability "${version}"
    echo "ETCD_MIGRATION_ROLLING_CHECKPOINT_OK version=${version}"
}

write_v2_dataset() {
    local endpoint
    local index key_id
    endpoint=$(endpoint_for 1)

    for index in $(seq 1 64); do
        printf -v key_id '%03d' "${index}"
        curl --fail --silent --show-error --request PUT \
            "${endpoint}/v2/keys/pasturestack/migration/key-${key_id}" \
            --data-urlencode "value=value-${key_id}" >/dev/null
    done

    curl --fail --silent --show-error --request PUT \
        "${endpoint}/v2/keys/pasturestack/migration/ttl-review" \
        --data-urlencode 'value=requires-expiry-policy' --data 'ttl=3600' >/dev/null
}

verify_v2_dataset() {
    local endpoint
    local index key_id actual
    endpoint=$(endpoint_for 1)

    for index in $(seq 1 64); do
        printf -v key_id '%03d' "${index}"
        actual=$(curl --fail --silent "${endpoint}/v2/keys/pasturestack/migration/key-${key_id}" | jq -r '.node.value')
        if [ "${actual}" != "value-${key_id}" ]; then
            echo "ETCD_MIGRATION_V2_VALUE_MISMATCH key=${key_id}" >&2
            return 1
        fi
    done
}

verify_v3_dataset() {
    local version=$1
    local endpoint=${2:-$(endpoint_for 1)}
    local ctl index key_id actual
    ctl=$(ctl_for "${version}")

    for index in $(seq 1 64); do
        printf -v key_id '%03d' "${index}"
        actual=$(ETCDCTL_API=3 "${ctl}" --endpoints="${endpoint}" \
            get "/pasturestack/migration/key-${key_id}" | sed -n '2p')
        if [ "${actual}" != "value-${key_id}" ]; then
            echo "ETCD_MIGRATION_V3_VALUE_MISMATCH key=${key_id} endpoint=${endpoint}" >&2
            return 1
        fi
    done
}

verify_v3_sentinel() {
    local version=$1
    local endpoint=${2:-$(endpoint_for 1)}
    local ctl actual
    ctl=$(ctl_for "${version}")
    actual=$(ETCDCTL_API=3 "${ctl}" --endpoints="${endpoint}" \
        get /pasturestack/migration/v3-sentinel | sed -n '2p')
    [ "${actual}" = "created-before-v3.2" ]
}

save_cluster_copy() {
    local name=$1
    stop_all
    mkdir -p "${work_root}/backups/${name}"
    cp -a "${work_root}/nodes/." "${work_root}/backups/${name}/"
}

restore_cluster_copy() {
    local name=$1
    local node_dir
    stop_all
    for node_dir in "${work_root}"/nodes/node1 "${work_root}"/nodes/node2 "${work_root}"/nodes/node3; do
        rm -rf -- "${node_dir:?}"
    done
    cp -a "${work_root}/backups/${name}/." "${work_root}/nodes/"
}

echo "ETCD_MIGRATION_GATE_START"
bash "${repo_root}/migration/fetch-checkpoints.sh"

docker image inspect "${compat_image}" >/dev/null || {
    echo "ETCD_MIGRATION_LOCAL_CANDIDATE_MISSING image=${compat_image}" >&2
    exit 1
}
compat_container=$(docker create --entrypoint /bin/true "${compat_image}")
docker cp "${compat_container}:/opt/pasturestack/etcd" "${work_root}/compat/etcd"
docker cp "${compat_container}:/opt/pasturestack/etcdctl" "${work_root}/compat/etcdctl"
docker rm "${compat_container}" >/dev/null
compat_container=""
chmod 0755 "${work_root}/compat/etcd" "${work_root}/compat/etcdctl"
"${work_root}/compat/etcd" --version 2>&1 | grep -F '2.3.7' >/dev/null

start_cluster 2.3.7 false 100000 new
write_v2_dataset
verify_v2_dataset

set +e
bash "${repo_root}/migration/preflight-v2.sh" "$(endpoint_for 1)" \
    "${evidence_dir}/preflight-ttl-blocked.json" >/dev/null
preflight_status=$?
set -e
if [ "${preflight_status}" -ne 2 ]; then
    echo "ETCD_MIGRATION_TTL_PREFLIGHT_DID_NOT_BLOCK status=${preflight_status}" >&2
    exit 1
fi

curl --fail --silent --request DELETE \
    "$(endpoint_for 1)/v2/keys/pasturestack/migration/ttl-review" >/dev/null
bash "${repo_root}/migration/preflight-v2.sh" "$(endpoint_for 1)" \
    "${evidence_dir}/preflight-ready.json" >/dev/null
if [ "$(jq -r '.leafKeys' "${evidence_dir}/preflight-ready.json")" -ne 64 ]; then
    echo "ETCD_MIGRATION_PREFLIGHT_KEY_COUNT_INVALID" >&2
    exit 1
fi

roll_cluster 3.0.17 true 100000
verify_v2_dataset
ETCDCTL_API=3 "$(ctl_for 3.0.17)" --endpoints="$(endpoint_for 1)" \
    put /pasturestack/migration/v3-sentinel created-before-v3.2 >/dev/null
verify_v3_sentinel 3.0.17

for checkpoint in 3.1.20 3.2.32 3.3.27 3.4.45; do
    roll_cluster "${checkpoint}" true 100000
    verify_v2_dataset
    verify_v3_sentinel "${checkpoint}"
done

save_cluster_copy pre-v2-to-v3
start_cluster 3.4.45 true 100000 existing

for index in 1 2 3; do
    stop_node "${index}"
    ETCDCTL_API=3 "$(ctl_for 3.4.45)" migrate \
        --data-dir="${work_root}/nodes/node${index}" \
        --wal-dir="${work_root}/nodes/node${index}/member/wal" \
        >"${work_root}/logs/migrate-node${index}.log" 2>&1
    start_node "${index}" 3.4.45 true 100000 existing
    wait_endpoint "${index}"
done
wait_cluster_health

for index in 1 2 3; do
    verify_v3_dataset 3.4.45 "$(endpoint_for "${index}")"
    verify_v3_sentinel 3.4.45 "$(endpoint_for "${index}")"
done

curl --fail --silent --request DELETE \
    "$(endpoint_for 1)/v2/keys/pasturestack?recursive=true" >/dev/null
bash "${repo_root}/migration/preflight-v2.sh" "$(endpoint_for 1)" \
    "${evidence_dir}/preflight-after-v2-removal.json" >/dev/null
if [ "$(jq -r '.leafKeys' "${evidence_dir}/preflight-after-v2-removal.json")" -ne 0 ]; then
    echo "ETCD_MIGRATION_V2_CUSTOM_DATA_REMAINS" >&2
    exit 1
fi

save_cluster_copy post-v2-to-v3

restore_cluster_copy pre-v2-to-v3
start_cluster 3.4.45 true 100000 existing
verify_v2_dataset
verify_v3_sentinel 3.4.45

restore_cluster_copy post-v2-to-v3
start_cluster 3.4.45 true 1 existing
verify_v3_dataset 3.4.45
verify_v3_sentinel 3.4.45
bash "${repo_root}/migration/preflight-v2.sh" "$(endpoint_for 1)" >/dev/null

for index in $(seq 1 8); do
    ETCDCTL_API=3 "$(ctl_for 3.4.45)" --endpoints="${all_endpoints}" \
        put "/pasturestack/migration/pre-3.5-snapshot-${index}" "${index}" >/dev/null
done
sleep 3
for index in 1 2 3; do
    stop_node "${index}"
    start_node "${index}" 3.4.45 true 100000 existing
    wait_endpoint "${index}"
done
wait_cluster_health
wait_cluster_version 3.4.45
wait_v3_capability 3.4.45

roll_cluster 3.5.33 true 1
for index in $(seq 1 8); do
    ETCDCTL_API=3 "$(ctl_for 3.5.33)" --endpoints="${all_endpoints}" \
        put "/pasturestack/migration/snapshot-trigger-${index}" "${index}" >/dev/null
done
sleep 3

for index in 1 2 3; do
    stop_node "${index}"
    "${cache_dir}/v3.5.33/etcdutl" check v2store \
        --data-dir="${work_root}/nodes/node${index}" \
        >"${evidence_dir}/v2store-node${index}.txt" 2>&1
    grep -F 'No custom content found in both v2store and WAL records' \
        "${evidence_dir}/v2store-node${index}.txt" >/dev/null
    start_node "${index}" 3.5.33 false 100000 existing
    wait_endpoint "${index}"
done
wait_cluster_health

for index in 1 2 3; do
    v2_status=$(curl --silent --output /dev/null --write-out '%{http_code}' \
        "$(endpoint_for "${index}")/v2/keys/")
    case "${v2_status}" in
        2*)
            echo "ETCD_MIGRATION_V2_API_STILL_ENABLED node=${index}" >&2
            exit 1
            ;;
    esac
done

roll_cluster 3.6.14 false 1
verify_v3_dataset 3.6.14
verify_v3_sentinel 3.6.14
wait_storage_version 3.6.14
for index in $(seq 1 8); do
    ETCDCTL_API=3 "$(ctl_for 3.6.14)" --endpoints="${all_endpoints}" \
        put "/pasturestack/migration/pre-3.7-snapshot-${index}" "${index}" >/dev/null
done
sleep 3
for index in 1 2 3; do
    stop_node "${index}"
    start_node "${index}" 3.6.14 false 10000 existing
    wait_endpoint "${index}"
done
wait_cluster_health
wait_cluster_version 3.6.14
wait_storage_version 3.6.14
wait_v3_capability 3.6.14
roll_cluster 3.7.1 false 10000
verify_v3_dataset 3.7.1
verify_v3_sentinel 3.7.1
ETCDCTL_API=3 "$(ctl_for 3.7.1)" --endpoints="${all_endpoints}" endpoint health \
    >"${evidence_dir}/endpoint-health.txt"

sha256sum "${repo_root}/migration/checkpoints.lock.tsv" \
    >"${evidence_dir}/checkpoints.lock.sha256"
cat > "${evidence_dir}/result.env" <<'EOF'
status=passed
source_package=2.3.8
source_engine=2.3.7
target=3.7.1
members=3
v2_application_keys=64
ttl_preflight=blocked-as-designed
full_cluster_restore=passed
mixed_version_quorum=passed
v2store_and_wal_clean=passed
v2_api_disabled=passed
storage_version_before_3.7=3.6.0
EOF

echo "ETCD_MIGRATION_GATE_OK source_package=2.3.8 source_engine=2.3.7 target=3.7.1 members=3 keys=64"
