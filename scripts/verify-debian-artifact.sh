#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
binary="${project_root}/bin/forms"
image="${FORMS_DEBIAN_IMAGE:-debian:13-slim}"

if [[ ! -x "${binary}" ]]; then
  printf 'باینری bin/forms پیدا نشد؛ ابتدا make build را اجرا کنید.\n' >&2
  exit 1
fi

docker run --rm --network none --read-only --user 65534:65534 \
  --cap-drop ALL --security-opt no-new-privileges \
  --mount "type=bind,src=${binary},dst=/usr/local/bin/forms,readonly" \
  "${image}" /usr/local/bin/forms version

printf 'باینری production روی %s تمیز، با کاربر غیرریشه، بدون شبکه و بدون runtime جانبی اجرا شد.\n' "${image}"
