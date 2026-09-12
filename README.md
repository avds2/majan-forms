# Persian Forms

[فارسی](README_FA.md)

A self-hosted, Persian-first online form service for a single organization.
It combines a Svelte administration interface, server-rendered public forms,
and a Go HTTP server in one static binary. PostgreSQL is the only required
external runtime service; Node.js and Go are build-time dependencies only.

The current codebase is distributed as a release candidate. Complete the
verification steps in the release and deployment guides before a production
rollout.

## Highlights

- Fully RTL Persian administration and public interfaces, Vazirmatn fonts, and
  Jalali date presentation while timestamps remain UTC in PostgreSQL.
- Internal users with Argon2id authentication and organization-wide admin,
  form-designer, and viewer roles; there is no public account registration.
- Revision-aware form drafts, 34 field and content-block types, accessible
  reordering, desktop/mobile preview, and three built-in themes.
- Typed branching rules, assignments, graph validation, route simulation, and
  deterministic fallback behavior.
- Immutable published snapshots, idempotent publishing, and the ability to
  pause and resume response collection without losing data.
- Multi-page public forms that work without JavaScript, with progressive
  enhancement, resumable server-side sessions, back navigation, and automatic
  removal of answers invalidated by a route change.
- Streaming uploads with staging/finalization/cleanup, drawing signatures with
  a keyboard-friendly PNG alternative, rate limiting, honeypots, minimum fill
  time, and optional server-verified Turnstile.
- Typed, server-authoritative submissions; response search and filters; soft
  deletion; multi-version reports; A4 print; and CSV, XLSX, and JSON exports.
- Checksummed project packages that transfer drafts and selected published
  versions without respondent data.
- Versioned full backups, verification and retention, offline restore with a
  safety backup, maintenance coordination, and systemd handoff.
- Liveness/readiness endpoints, an administrative health page, `doctor`,
  cleanup, backup timers, and hardened systemd units.

## Architecture

```text
Browser
  ├─ Svelte admin UI ─┐
  └─ public HTML/JS ──┼─ Go net/http binary ── PostgreSQL
                      ├─ embedded assets and migrations
                      ├─ private upload/secret storage
                      └─ optional OpenAI-compatible provider
```

The public form is server rendered and progressively enhanced. The admin
application is compiled at build time and embedded in the binary. Domain logic
and SQL live in bounded Go packages rather than handlers. Published forms are
immutable snapshots, and public sessions remain pinned to the version on which
they started. See the [development guide](docs/DEVELOPMENT.md) for the detailed
architecture and implementation boundaries.

## Quick start for development

Build prerequisites are Go 1.27.1, Node.js 24 with npm, Docker with Compose,
and standard Unix build tools. Docker is used here only to start PostgreSQL.

```bash
docker compose -f compose.dev.yml up -d
make deps
make build VERSION=0.4.0-dev COMMIT=local

export FORMS_DATABASE_URL='postgres://forms:forms-dev-only@127.0.0.1:5432/forms?sslmode=disable'
export FORMS_DATA_DIR="$PWD/var"
export FORMS_BACKUP_DIR="$PWD/backups"
export FORMS_INSECURE_COOKIE=true
install -d -m 0700 "$FORMS_DATA_DIR" "$FORMS_BACKUP_DIR"

./bin/forms migrate up
./bin/forms admin create \
  --email admin@example.test \
  --username admin \
  --name 'Administrator'
./bin/forms serve
```

`admin create` securely prompts for the password. For non-interactive use,
provide a root-controlled `0600` file through `--password-file` instead of a
command-line password.

Open `http://127.0.0.1:8080`. Readiness is available at
`http://127.0.0.1:8080/health/ready`; a published project is served at
`/f/<slug>`. `FORMS_INSECURE_COOKIE=true` is strictly for local HTTP. Never set
it in production.

For production, use the from-scratch [Debian installation guide](docs/INSTALL-DEBIAN.md).
The release layout and verification procedure are documented in
[RELEASE-FA.md](docs/RELEASE-FA.md).

## Optional AI data assistant

AI is disabled by default, and the application makes no provider request while
it is disabled. An administrator may configure an OpenAI Responses API or Chat
Completions-compatible endpoint and must pass the built-in streaming/tool
compatibility test. No model or hosted AI service is bundled with this project.

The assistant is read-only: it can call a small allowlist of server-side,
parameterized aggregation tools and cannot submit arbitrary SQL or modify,
delete, or export raw data. Consent is enabled separately per project. Raw text
sampling requires a second explicit consent and is off by default. The API key
is stored as a private `0600` file under `FORMS_DATA_DIR`, is excluded from
PostgreSQL and backups, and is protected by endpoint/redirect/SSRF checks.

## Administrative CLI

Help and version information work without database configuration:

```bash
./bin/forms --help
./bin/forms version
./bin/forms admin --help
./bin/forms backup --help
./bin/forms restore --help
```

The CLI covers:

- `serve`
- `migrate status|up`
- `admin create|reset-password`
- `backup create|verify|list|prune`
- `restore verify|prepare|apply|pending`
- `cleanup`
- `doctor`
- `version`

Restore is intentionally offline and requires explicit confirmation. Follow
[the backup and restore runbook](docs/BACKUP-RESTORE-FA.md), especially for
rollback after a forward-only migration.

## Tests and release verification

```bash
make test

export FORMS_TEST_DATABASE_URL='postgres://forms:forms-dev-only@127.0.0.1:5432/forms?sslmode=disable'
make integration
GOCACHE=/tmp/forms-go-cache go test -race ./...

npm --prefix web/admin audit
make build VERSION=0.4.0-dev COMMIT=local
FORMS_DATABASE_URL="$FORMS_TEST_DATABASE_URL" ./scripts/measure-idle-memory.sh
./scripts/verify-debian-artifact.sh
make systemd-check
```

Additional opt-in checks exercise the real systemd units in an isolated Debian
container, clean Debian 12/13 installation flows, and Firefox/WebKit UI and
accessibility paths. Commands and required environment variables are listed in
[the development guide](docs/DEVELOPMENT.md).

The build enforces gzip budgets of 180 KiB for total admin JavaScript/CSS and
50 KiB for public-form JavaScript. Release archives include internal
`SHA256SUMS`, an external archive checksum, a CycloneDX 1.5 SBOM, deployment
units, reverse-proxy examples, documentation, the MIT project license, and the
font license.

## Documentation

- [Debian installation](docs/INSTALL-DEBIAN.md)
- [Configuration reference](docs/CONFIGURATION-FA.md)
- [Administrator guide and CLI summary](docs/ADMIN-GUIDE-FA.md)
- [Backup and restore runbook](docs/BACKUP-RESTORE-FA.md)
- [Upgrade and rollback](docs/UPGRADE-FA.md)
- [Troubleshooting](docs/TROUBLESHOOTING-FA.md)
- [Security model](docs/SECURITY.md)
- [Development and verification](docs/DEVELOPMENT.md)
- [Release procedure](docs/RELEASE-FA.md)

Most operational documentation is currently in Persian because the product and
its primary deployment audience are Persian-speaking.

## License

The application source is released under the [MIT License](LICENSE). The
bundled Vazirmatn font is licensed separately under the SIL Open Font License;
its license is available at [assets/fonts/OFL.txt](assets/fonts/OFL.txt).
