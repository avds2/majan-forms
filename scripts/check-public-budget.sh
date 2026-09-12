#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
asset_dir="${project_root}/internal/web/public_assets"
javascript_budget="${FORMS_PUBLIC_JS_GZIP_BUDGET:-51200}"

javascript_bytes="$(gzip -9 -c "${asset_dir}/forms.js" | wc -c)"
css_bytes="$(gzip -9 -c "${asset_dir}/forms.css" | wc -c)"

printf 'Public JS gzip: %d bytes از بودجهٔ %d bytes\n' "${javascript_bytes}" "${javascript_budget}"
printf 'Public CSS gzip (ثبت اطلاعاتی): %d bytes\n' "${css_bytes}"

if [[ "${javascript_bytes}" -gt "${javascript_budget}" ]]; then
  printf 'بودجهٔ JavaScript فرم عمومی رد شد.\n' >&2
  exit 1
fi
