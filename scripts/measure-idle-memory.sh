#!/usr/bin/env bash
set -euo pipefail

: "${FORMS_DATABASE_URL:?FORMS_DATABASE_URL الزامی است}"

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
forms_binary="${FORMS_BINARY:-${project_root}/bin/forms}"
http_addr="${FORMS_MEMORY_HTTP_ADDR:-127.0.0.1:18081}"
probe_url="http://${http_addr}/health/ready"
temporary_dir="$(mktemp -d)"
server_pid=""

cleanup() {
  if [[ -n "${server_pid}" ]] && kill -0 "${server_pid}" 2>/dev/null; then
    kill -TERM "${server_pid}" 2>/dev/null || true
    wait "${server_pid}" 2>/dev/null || true
  fi
  rm -rf -- "${temporary_dir}"
}
trap cleanup EXIT

FORMS_HTTP_ADDR="${http_addr}" \
FORMS_DATA_DIR="${temporary_dir}/data" \
FORMS_INSECURE_COOKIE=true \
  "${forms_binary}" serve >"${temporary_dir}/server.log" 2>&1 &
server_pid="$!"

ready=false
for _ in $(seq 1 50); do
  if curl --fail --silent --output /dev/null "${probe_url}"; then
    ready=true
    break
  fi
  if ! kill -0 "${server_pid}" 2>/dev/null; then
    cat "${temporary_dir}/server.log" >&2
    exit 1
  fi
  sleep 0.1
done
if [[ "${ready}" != true ]]; then
  printf 'سرویس در مهلت تعیین‌شده آماده نشد.\n' >&2
  cat "${temporary_dir}/server.log" >&2
  exit 1
fi

sleep 2
rss_kib="$(awk '/^VmRSS:/ { print $2 }' "/proc/${server_pid}/status")"
if [[ -z "${rss_kib}" ]]; then
  printf 'اندازه‌گیری VmRSS ناموفق بود.\n' >&2
  exit 1
fi
printf 'Idle VmRSS: %s KiB (%s MiB)\n' "${rss_kib}" "$(( (rss_kib + 1023) / 1024 ))"

kill -TERM "${server_pid}"
wait "${server_pid}"
server_pid=""
