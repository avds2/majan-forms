#!/usr/bin/env bash
set -euo pipefail

if [[ ! -x /source/forms ]]; then
  printf 'باینری آزمون systemd در /source/forms پیدا نشد.\n' >&2
  exit 1
fi

if ! getent group forms >/dev/null; then
  addgroup --system forms
fi
if ! getent passwd forms >/dev/null; then
  adduser --system --ingroup forms --home /var/lib/forms --no-create-home forms
fi

install -o root -g root -m 0755 /source/forms /usr/local/bin/forms
install -d -o root -g forms -m 0750 /etc/forms
install -d -o forms -g forms -m 0700 /var/lib/forms /var/backups/forms
for unit in /source/systemd/forms.service /source/systemd/forms-backup.service \
  /source/systemd/forms-backup.timer /source/systemd/forms-cleanup.service \
  /source/systemd/forms-cleanup.timer; do
  install -o root -g root -m 0644 "${unit}" "/etc/systemd/system/${unit##*/}"
done

install -o root -g forms -m 0640 /dev/null /etc/forms/forms.env
printf '%s\n' \
  'FORMS_DATABASE_URL=postgres:///forms?host=/var/run/postgresql&sslmode=disable' \
  'FORMS_HTTP_ADDR=127.0.0.1:8080' \
  'FORMS_DATA_DIR=/var/lib/forms' \
  'FORMS_BACKUP_DIR=/var/backups/forms' \
  'FORMS_TIMEZONE=Asia/Tehran' \
  'FORMS_LOG_LEVEL=info' \
  'FORMS_DB_MAX_CONNS=6' \
  'FORMS_DB_MIN_CONNS=0' \
  'FORMS_INSECURE_COOKIE=true' \
  >/etc/forms/forms.env

systemctl daemon-reload
systemctl enable --now postgresql.service
for _ in $(seq 1 80); do
  pg_isready -q && break
  sleep 0.25
done
pg_isready -q

if ! runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='forms'" | grep -qx 1; then
  runuser -u postgres -- createuser forms
fi
if ! runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_database WHERE datname='forms'" | grep -qx 1; then
  runuser -u postgres -- createdb --owner=forms forms
fi

run_forms() {
  runuser -u forms -- env \
    FORMS_DATABASE_URL='postgres:///forms?host=/var/run/postgresql&sslmode=disable' \
    FORMS_HTTP_ADDR='127.0.0.1:8080' \
    FORMS_DATA_DIR='/var/lib/forms' \
    FORMS_BACKUP_DIR='/var/backups/forms' \
    FORMS_TIMEZONE='Asia/Tehran' \
    FORMS_LOG_LEVEL='info' \
    FORMS_DB_MAX_CONNS='6' \
    FORMS_DB_MIN_CONNS='0' \
    FORMS_INSECURE_COOKIE='true' \
    /usr/local/bin/forms "$@"
}

run_forms migrate up
install -o forms -g forms -m 0600 /dev/null /var/lib/forms/admin-password
printf '%s\n' 'Systemd-Runtime-Review-2026!' >/var/lib/forms/admin-password
run_forms admin create \
  --email systemd@example.test \
  --username systemd-admin \
  --name 'مدیر آزمون systemd' \
  --password-file /var/lib/forms/admin-password
rm -f /var/lib/forms/admin-password

systemctl enable --now forms.service forms-backup.timer forms-cleanup.timer
for _ in $(seq 1 80); do
  curl --fail --silent http://127.0.0.1:8080/health/ready >/dev/null && break
  sleep 0.25
done
curl --fail --silent http://127.0.0.1:8080/health/ready >/dev/null

systemctl start forms-backup.service forms-cleanup.service
backup_path="$(find /var/backups/forms -maxdepth 1 -type f -name 'forms-*.tar.gz' | head -n 1)"
test -n "${backup_path}"
run_forms backup verify "${backup_path}"

# Exercise timer dispatch itself with short-lived test-only overrides.  Merely
# starting the oneshot units does not prove that systemd can schedule them.
backup_count_before="$(find /var/backups/forms -maxdepth 1 -type f -name 'forms-*.tar.gz' | wc -l)"
install -d -o root -g root -m 0755 \
  /etc/systemd/system/forms-backup.timer.d \
  /etc/systemd/system/forms-cleanup.timer.d
printf '%s\n' \
  '[Timer]' \
  'OnCalendar=' \
  'OnActiveSec=2s' \
  'RandomizedDelaySec=0' \
  'AccuracySec=1s' \
  >/etc/systemd/system/forms-backup.timer.d/runtime-test.conf
printf '%s\n' \
  '[Timer]' \
  'OnBootSec=' \
  'OnUnitActiveSec=' \
  'OnActiveSec=2s' \
  'RandomizedDelaySec=0' \
  'AccuracySec=1s' \
  >/etc/systemd/system/forms-cleanup.timer.d/runtime-test.conf
systemctl daemon-reload
systemctl restart forms-backup.timer forms-cleanup.timer

timers_dispatched=false
for _ in $(seq 1 120); do
  backup_count_after="$(find /var/backups/forms -maxdepth 1 -type f -name 'forms-*.tar.gz' | wc -l)"
  backup_trigger="$(systemctl show --property=LastTriggerUSecMonotonic --value forms-backup.timer)"
  cleanup_trigger="$(systemctl show --property=LastTriggerUSecMonotonic --value forms-cleanup.timer)"
  if [[ "${backup_count_after}" -gt "${backup_count_before}" \
    && "${backup_trigger}" != 0 \
    && "${cleanup_trigger}" != 0 \
    && "$(systemctl show --property=Result --value forms-backup.service)" == success \
    && "$(systemctl show --property=Result --value forms-cleanup.service)" == success ]]; then
    timers_dispatched=true
    break
  fi
  sleep 0.25
done
if [[ "${timers_dispatched}" != true ]]; then
  systemctl status --no-pager forms-backup.timer forms-cleanup.timer \
    forms-backup.service forms-cleanup.service >&2 || true
  exit 1
fi

# Restore the production schedules before the simulated reboot checks.
systemctl stop forms-backup.timer forms-cleanup.timer
unlink /etc/systemd/system/forms-backup.timer.d/runtime-test.conf
unlink /etc/systemd/system/forms-cleanup.timer.d/runtime-test.conf
rmdir /etc/systemd/system/forms-backup.timer.d \
  /etc/systemd/system/forms-cleanup.timer.d
systemctl daemon-reload
systemctl start forms-backup.timer forms-cleanup.timer
printf 'اجرای خودکار هر دو job توسط timer با موفقیت تأیید شد.\n'

systemctl is-active --quiet forms.service
systemctl is-active --quiet forms-backup.timer
systemctl is-active --quiet forms-cleanup.timer
systemctl list-timers --no-pager forms-backup.timer forms-cleanup.timer
printf 'راه‌اندازی اولیه، timerها و اجرای دستی jobها موفق بود.\n'
