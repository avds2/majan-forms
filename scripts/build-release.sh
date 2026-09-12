#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="${1:-}"
commit="${2:-unknown}"
build_date="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
output_dir="${FORMS_RELEASE_DIR:-${project_root}/dist}"

if [[ ! "${version}" =~ ^[0-9A-Za-z][0-9A-Za-z._-]{0,63}$ ]]; then
  printf 'نسخهٔ release معتبر نیست. نمونه: 0.4.0-rc1\n' >&2
  exit 2
fi

mkdir -p "${output_dir}"
staging="$(mktemp -d)"
cleanup() { rm -rf -- "${staging}"; }
trap cleanup EXIT

make -C "${project_root}" build VERSION="${version}" COMMIT="${commit}" BUILD_DATE="${build_date}"

release_root="${staging}/forms-${version}"
install -d -m 0755 "${release_root}/bin" "${release_root}/deploy" "${release_root}/docs" "${release_root}/licenses"
install -m 0755 "${project_root}/bin/forms" "${release_root}/bin/forms"
cp -a "${project_root}/deploy/." "${release_root}/deploy/"
cp -a "${project_root}/docs/." "${release_root}/docs/"
install -m 0644 \
  "${project_root}/README.md" \
  "${project_root}/README_FA.md" \
  "${project_root}/PROJECT_PLAN_FA.md" \
  "${project_root}/LICENSE" \
  "${release_root}/"
install -m 0644 "${project_root}/assets/fonts/OFL.txt" "${release_root}/licenses/Vazirmatn-OFL.txt"

go run "${project_root}/scripts/release_sbom.go" \
  "${release_root}/bin/forms" "${project_root}/web/admin/package-lock.json" "${version}" "${build_date}" \
  >"${release_root}/sbom.cdx.json"

(
  cd "${release_root}"
  find . -type f -print0 | sort -z | xargs -0 sha256sum >"${staging}/SHA256SUMS"
)
install -m 0644 "${staging}/SHA256SUMS" "${release_root}/SHA256SUMS"

archive="${output_dir}/forms-${version}-linux-amd64.tar.gz"
partial="${archive}.partial"
if [[ -e "${archive}" || -e "${archive}.sha256" || -e "${partial}" ]]; then
  printf 'خروجی release از قبل وجود دارد: %s\n' "${archive}" >&2
  exit 1
fi
tar --sort=name --mtime="${build_date}" --owner=0 --group=0 --numeric-owner -C "${staging}" -cf - "forms-${version}" | gzip -n -9 >"${partial}"
chmod 0644 "${partial}"
mv "${partial}" "${archive}"
(
  cd "${output_dir}"
  sha256sum "$(basename "${archive}")" >"$(basename "${archive}").sha256"
)

if [[ -n "${FORMS_MINISIGN_SECRET_KEY:-}" ]]; then
  command -v minisign >/dev/null
  minisign -S -s "${FORMS_MINISIGN_SECRET_KEY}" -m "${archive}" -x "${archive}.minisig"
fi

printf 'Release: %s\nChecksum: %s.sha256\nSBOM: sbom.cdx.json inside archive\n' "${archive}" "${archive}"
