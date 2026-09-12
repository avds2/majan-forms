#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
units=(
  "${project_root}/deploy/systemd/forms.service"
  "${project_root}/deploy/systemd/forms-backup.service"
  "${project_root}/deploy/systemd/forms-backup.timer"
  "${project_root}/deploy/systemd/forms-cleanup.service"
  "${project_root}/deploy/systemd/forms-cleanup.timer"
)

output="$(systemd-analyze verify "${units[@]}" 2>&1 || true)"
unexpected=false
while IFS= read -r line; do
  [[ -z "${line}" ]] && continue
  case "${line}" in
    *"Command /usr/local/bin/forms is not executable: No such file or directory"*) ;;
    *"Unit postgresql.service not found."*) ;;
    *)
      printf '%s\n' "${line}" >&2
      unexpected=true
      ;;
  esac
done <<<"${output}"

if [[ "${unexpected}" == true ]]; then
  printf 'واحدهای systemd هشدار یا خطای پیش‌بینی‌نشده دارند.\n' >&2
  exit 1
fi
printf 'ساختار واحدهای systemd معتبر است؛ وابستگی‌های میزبان هنگام نصب کنترل می‌شوند.\n'
