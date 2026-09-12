#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
binary="${FORMS_BINARY:-${project_root}/bin/forms}"
image="${FORMS_SYSTEMD_IMAGE:-forms-systemd-runtime:debian13}"
container="forms-systemd-$RANDOM-$$"
docker_config="${DOCKER_CONFIG:-${TMPDIR:-/tmp}/forms-systemd-docker-config}"

if [[ ! -x "${binary}" || "${container}" != forms-systemd-* ]]; then
  printf 'باینری یا شناسهٔ آزمون معتبر نیست.\n' >&2
  exit 1
fi

cleanup() {
  docker rm -f "${container}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

mkdir -p "${docker_config}"
export DOCKER_CONFIG="${docker_config}"
docker build --tag "${image}" --file "${project_root}/scripts/systemd-runtime.Dockerfile" "${project_root}"
docker run --detach --privileged --cgroupns=private \
  --name "${container}" \
  --tmpfs /run --tmpfs /run/lock \
  --mount "type=bind,src=${binary},dst=/source/forms,readonly" \
  --mount "type=bind,src=${project_root}/deploy/systemd,dst=/source/systemd,readonly" \
  --mount "type=bind,src=${project_root}/scripts/systemd-runtime-setup.sh,dst=/source/setup.sh,readonly" \
  "${image}" >/dev/null

systemd_ready=false
for _ in $(seq 1 120); do
  state="$(docker exec "${container}" systemctl is-system-running 2>/dev/null || true)"
  if [[ "${state}" == running || "${state}" == degraded ]]; then
    systemd_ready=true
    break
  fi
  sleep 0.5
done
if [[ "${systemd_ready}" != true ]]; then
  docker logs "${container}" >&2
  exit 1
fi

docker exec "${container}" /source/setup.sh

docker exec "${container}" systemctl restart forms.service
docker exec "${container}" curl --fail --retry 20 --retry-delay 1 --retry-connrefused --silent \
  http://127.0.0.1:8080/health/ready >/dev/null
printf 'restart سرویس با موفقیت تأیید شد.\n'

docker restart --time 30 "${container}" >/dev/null
for _ in $(seq 1 120); do
  if docker exec "${container}" curl --fail --silent http://127.0.0.1:8080/health/ready >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
docker exec "${container}" curl --fail --silent http://127.0.0.1:8080/health/ready >/dev/null
docker exec "${container}" systemctl is-active --quiet postgresql.service
docker exec "${container}" systemctl is-active --quiet forms.service
docker exec "${container}" systemctl is-active --quiet forms-backup.timer
docker exec "${container}" systemctl is-active --quiet forms-cleanup.timer
docker exec "${container}" systemctl list-timers --no-pager forms-backup.timer forms-cleanup.timer
printf 'بوت مجدد container و بازگشت خودکار سرویس و timerها با موفقیت تأیید شد.\n'
