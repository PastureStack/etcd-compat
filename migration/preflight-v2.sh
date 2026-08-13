#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
    echo "usage: $0 <http-or-https-endpoint> [result.json]" >&2
    exit 64
fi

endpoint=${1%/}
result_path=${2:-}

case "${endpoint}" in
    http://*|https://*) ;;
    *)
        echo "ETCD_V2_PREFLIGHT_ENDPOINT_INVALID" >&2
        exit 64
        ;;
esac

for command_name in curl jq; do
    command -v "${command_name}" >/dev/null 2>&1 || {
        echo "ETCD_V2_PREFLIGHT_REQUIRED_COMMAND_MISSING command=${command_name}" >&2
        exit 1
    }
done

curl_tls_args=()
if [ -n "${ETCD_MIGRATION_CACERT:-}" ]; then
    test -r "${ETCD_MIGRATION_CACERT}" || {
        echo "ETCD_V2_PREFLIGHT_CACERT_UNREADABLE" >&2
        exit 1
    }
    curl_tls_args+=(--cacert "${ETCD_MIGRATION_CACERT}")
fi
if [ -n "${ETCD_MIGRATION_CERT:-}" ] || [ -n "${ETCD_MIGRATION_KEY:-}" ]; then
    if [ -z "${ETCD_MIGRATION_CERT:-}" ] || [ -z "${ETCD_MIGRATION_KEY:-}" ]; then
        echo "ETCD_V2_PREFLIGHT_CLIENT_IDENTITY_INCOMPLETE" >&2
        exit 1
    fi
    test -r "${ETCD_MIGRATION_CERT}" && test -r "${ETCD_MIGRATION_KEY}" || {
        echo "ETCD_V2_PREFLIGHT_CLIENT_IDENTITY_UNREADABLE" >&2
        exit 1
    }
    curl_tls_args+=(--cert "${ETCD_MIGRATION_CERT}" --key "${ETCD_MIGRATION_KEY}")
fi

health=$(curl --fail --silent --show-error "${curl_tls_args[@]}" "${endpoint}/health")
if [ "$(jq -r '.health // false' <<<"${health}")" != "true" ]; then
    echo "ETCD_V2_PREFLIGHT_ENDPOINT_UNHEALTHY" >&2
    exit 1
fi

set +e
response=$(curl --silent --show-error --write-out $'\n%{http_code}' \
    "${curl_tls_args[@]}" "${endpoint}/v2/keys/?recursive=true&sorted=true")
curl_status=$?
set -e
if [ "${curl_status}" -ne 0 ]; then
    echo "ETCD_V2_PREFLIGHT_READ_FAILED" >&2
    exit 1
fi

http_status=${response##*$'\n'}
body=${response%$'\n'*}
if [ "${http_status}" = "404" ]; then
    body='{"node":{"dir":true,"nodes":[]}}'
elif [ "${http_status}" != "200" ]; then
    echo "ETCD_V2_PREFLIGHT_HTTP_ERROR status=${http_status}" >&2
    exit 1
fi

result=$(jq -c '
    [.. | objects | select(has("key") and ((.dir // false) | not))] as $keys
    | {
        leafKeys: ($keys | length),
        ttlKeys: ($keys | map(select(has("expiration") or ((.ttl // 0) > 0))) | length),
        decision: (if ($keys | map(select(has("expiration") or ((.ttl // 0) > 0))) | length) == 0 then "ready" else "blocked" end)
      }
' <<<"${body}")

if [ -n "${result_path}" ]; then
    mkdir -p "$(dirname "${result_path}")"
    printf '%s\n' "${result}" > "${result_path}"
fi

printf '%s\n' "${result}"
if [ "$(jq -r '.ttlKeys' <<<"${result}")" -ne 0 ]; then
    echo "ETCD_V2_PREFLIGHT_TTL_POLICY_REQUIRED" >&2
    exit 2
fi
