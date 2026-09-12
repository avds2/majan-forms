#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
image="${FORMS_PLAYWRIGHT_IMAGE:-mcr.microsoft.com/playwright:v1.58.2-noble}"
base_url="${FORMS_BROWSER_BASE_URL:-http://127.0.0.1:18081}"
npm_cache="${FORMS_PLAYWRIGHT_NPM_CACHE:-${TMPDIR:-/tmp}/forms-playwright-npm-cache}"

if [[ -z "${FORMS_BROWSER_USERNAME:-}" || -z "${FORMS_BROWSER_PASSWORD:-}" ]]; then
  printf 'FORMS_BROWSER_USERNAME و FORMS_BROWSER_PASSWORD الزامی‌اند.\n' >&2
  exit 2
fi

mkdir -p "${npm_cache}"
docker run --rm --init --ipc=host --network=host \
  --mount "type=bind,src=${project_root}/scripts/cross-browser-check.mjs,dst=/source/cross-browser-check.mjs,readonly" \
  --mount "type=bind,src=${npm_cache},dst=/root/.npm" \
  -e "FORMS_BROWSER_BASE_URL=${base_url}" \
  -e "FORMS_BROWSER_USERNAME=${FORMS_BROWSER_USERNAME}" \
  -e "FORMS_BROWSER_PASSWORD=${FORMS_BROWSER_PASSWORD}" \
  -e PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 \
  "${image}" sh -euc '
    runner_dir="$(mktemp -d)"
    trap '\''rm -rf -- "${runner_dir}"'\'' EXIT
    cd "${runner_dir}"
    npm init -y >/dev/null
    npm install --no-save --no-audit --no-fund playwright@1.58.2 axe-core@4.10.2 >/dev/null
    cp /source/cross-browser-check.mjs .
    node cross-browser-check.mjs
  '
