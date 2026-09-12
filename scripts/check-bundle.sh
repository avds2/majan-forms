#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="${project_root}/internal/web/admin_dist/assets"
budget_bytes="${FORMS_ADMIN_JS_CSS_GZIP_BUDGET:-184320}"

if [[ ! -d "${dist_dir}" ]]; then
  printf 'خروجی فرانت‌اند پیدا نشد؛ ابتدا npm run build را اجرا کنید.\n' >&2
  exit 1
fi

total_bytes=0
asset_count=0
while IFS= read -r -d '' asset; do
  compressed_bytes="$(gzip -9 -c "${asset}" | wc -c)"
  total_bytes="$((total_bytes + compressed_bytes))"
  asset_count="$((asset_count + 1))"
done < <(find "${dist_dir}" -maxdepth 1 -type f \( -name '*.js' -o -name '*.css' \) -print0)

if [[ "${asset_count}" -eq 0 ]]; then
  printf 'هیچ فایل JS/CSS برای اندازه‌گیری پیدا نشد.\n' >&2
  exit 1
fi

printf 'Admin JS/CSS gzip: %d bytes از بودجهٔ %d bytes\n' "${total_bytes}" "${budget_bytes}"
if [[ "${total_bytes}" -gt "${budget_bytes}" ]]; then
  printf 'بودجهٔ bundle رد شد.\n' >&2
  exit 1
fi
