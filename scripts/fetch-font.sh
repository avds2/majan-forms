#!/usr/bin/env bash
set -euo pipefail

font_version="v33.003"
archive_sha256="0a9afd41967e6f57096a56a181a23f81a2b999b62f1f2a4e4b26736580854fdb"
archive_url="https://github.com/rastikerdar/vazirmatn/releases/download/${font_version}/vazirmatn-${font_version}.zip"
project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
font_dir="${project_root}/assets/fonts"
temporary_dir="$(mktemp -d)"
trap 'rm -rf "${temporary_dir}"' EXIT

archive_path="${temporary_dir}/vazirmatn.zip"
curl --fail --location --silent --show-error --output "${archive_path}" "${archive_url}"
printf '%s  %s\n' "${archive_sha256}" "${archive_path}" | sha256sum --check --status

mkdir -p "${font_dir}"
unzip -p "${archive_path}" 'fonts/webfonts/Vazirmatn-Regular.woff2' > "${temporary_dir}/Vazirmatn-Regular.woff2"
unzip -p "${archive_path}" 'fonts/webfonts/Vazirmatn-SemiBold.woff2' > "${temporary_dir}/Vazirmatn-SemiBold.woff2"
unzip -p "${archive_path}" 'OFL.txt' > "${temporary_dir}/OFL.txt"
install -m 0644 "${temporary_dir}/Vazirmatn-Regular.woff2" "${font_dir}/Vazirmatn-Regular.woff2"
install -m 0644 "${temporary_dir}/Vazirmatn-SemiBold.woff2" "${font_dir}/Vazirmatn-SemiBold.woff2"
install -m 0644 "${temporary_dir}/OFL.txt" "${font_dir}/OFL.txt"

printf 'Vazirmatn %s با checksum تأییدشده نصب شد.\n' "${font_version}"

