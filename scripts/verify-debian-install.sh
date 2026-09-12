#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
binary="${FORMS_BINARY:-${project_root}/bin/forms}"
image="${FORMS_DEBIAN_IMAGE:-debian:13-slim}"
postgres_image="${FORMS_POSTGRES_IMAGE:-postgres:17-alpine}"
run_id="forms-install-$RANDOM-$$"
network_name="${run_id}-network"
database_container="${run_id}-postgres"

if [[ ! -x "${binary}" || "${run_id}" != forms-install-* ]]; then
  printf 'باینری یا شناسهٔ آزمون معتبر نیست.\n' >&2
  exit 1
fi

cleanup() {
  docker rm -f "${database_container}" >/dev/null 2>&1 || true
  docker network rm "${network_name}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker network create "${network_name}" >/dev/null
docker run -d --name "${database_container}" --network "${network_name}" \
  -e POSTGRES_DB=forms -e POSTGRES_USER=forms -e POSTGRES_PASSWORD=forms-install-test \
  "${postgres_image}" >/dev/null

ready=false
for _ in $(seq 1 60); do
  if docker exec "${database_container}" pg_isready -U forms -d forms >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 0.5
done
if [[ "${ready}" != true ]]; then
  docker logs "${database_container}" >&2
  exit 1
fi

docker run --rm --network "${network_name}" \
  --mount "type=bind,src=${binary},dst=/source/forms,readonly" \
  -e FORMS_DATABASE_URL="postgres://forms:forms-install-test@${database_container}:5432/forms?sslmode=disable" \
  "${image}" sh -euc '
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -qq -y -o Dpkg::Use-Pty=0 --no-install-recommends adduser ca-certificates curl postgresql-client
    printf "[install] prerequisites ready\n"
    adduser --system --group --home /var/lib/forms --no-create-home forms
    install -o root -g root -m 0755 /source/forms /usr/local/bin/forms
    install -d -o forms -g forms -m 0700 /var/lib/forms /var/backups/forms
    install -o forms -g forms -m 0600 /dev/null /var/lib/forms/admin-password
    printf "%s\n" "Debian-Install-Review-2026!" >/var/lib/forms/admin-password
    common_env="FORMS_DATABASE_URL=${FORMS_DATABASE_URL} FORMS_DATA_DIR=/var/lib/forms FORMS_BACKUP_DIR=/var/backups/forms FORMS_HTTP_ADDR=127.0.0.1:8080 FORMS_INSECURE_COOKIE=true"
    runuser -u forms -- env ${common_env} /usr/local/bin/forms migrate up
    runuser -u forms -- env ${common_env} /usr/local/bin/forms admin create --email install@example.test --username install-admin --name "مدیر نصب" --password-file /var/lib/forms/admin-password
    printf "[install] database and administrator ready\n"
    rm -f /var/lib/forms/admin-password
    runuser -u forms -- env ${common_env} /usr/local/bin/forms serve >/tmp/forms-server.log 2>&1 &
    server_pid=$!
    ready=false
    for _ in $(seq 1 80); do
      if curl --fail --silent http://127.0.0.1:8080/health/ready >/dev/null; then ready=true; break; fi
      if ! kill -0 "${server_pid}" 2>/dev/null; then cat /tmp/forms-server.log >&2; exit 1; fi
      sleep 0.25
    done
    test "${ready}" = true
    printf "[install] health endpoint ready\n"
    kill -TERM "${server_pid}"
    wait "${server_pid}" || test "$?" -eq 143
    runuser -u forms -- env ${common_env} /usr/local/bin/forms backup create
    backup_path="$(find /var/backups/forms -maxdepth 1 -type f -name "forms-*.tar.gz" | head -n 1)"
    test -n "${backup_path}"
    runuser -u forms -- env ${common_env} /usr/local/bin/forms backup verify "${backup_path}"
    runuser -u forms -- env ${common_env} /usr/local/bin/forms restore apply --confirm "${backup_path}"
    runuser -u forms -- env ${common_env} /usr/local/bin/forms doctor
    printf "[install] backup, restore and doctor passed\n"
  '

printf 'نصب و گردش عملیاتی روی %s با موفقیت تأیید شد.\n' "${image}"
