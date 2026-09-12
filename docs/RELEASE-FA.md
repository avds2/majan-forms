# ساخت و تأیید release

release روی ماشین build مورد اعتماد و با lockfile موجود ساخته می‌شود:

```bash
make deps
make test
FORMS_TEST_DATABASE_URL='postgres://...' make integration
BUILD_DATE='2026-09-09T06:20:00Z' make release VERSION=0.4.0-rc1 COMMIT='<git-commit>'
```

خروجی `dist/forms-VERSION-linux-amd64.tar.gz` شامل باینری، unitهای systemd، نمونه‌های Caddy/Nginx، مستندات، مجوز MIT پروژه، مجوز فونت، `SHA256SUMS` داخلی و CycloneDX 1.5 SBOM است. checksum کل آرشیو کنار آن نوشته می‌شود. script روی خروجی موجود overwrite نمی‌کند.

اگر minisign در runner نصب و `FORMS_MINISIGN_SECRET_KEY` به مسیر کلید خصوصی محدود اشاره کند، فایل `.minisig` نیز ساخته می‌شود. کلید خصوصی، environment، data directory و backup واقعی هرگز داخل release قرار نمی‌گیرند.

کنترل artifact:

```bash
(cd dist && sha256sum --check forms-VERSION-linux-amd64.tar.gz.sha256)
tar -tzf dist/forms-VERSION-linux-amd64.tar.gz
./scripts/verify-debian-artifact.sh
./scripts/verify-systemd-units.sh
./scripts/verify-systemd-runtime.sh
FORMS_DEBIAN_IMAGE=debian:13-slim FORMS_POSTGRES_IMAGE=postgres:17-alpine ./scripts/verify-debian-install.sh
FORMS_DEBIAN_IMAGE=debian:12-slim FORMS_POSTGRES_IMAGE=postgres:15-alpine ./scripts/verify-debian-install.sh
FORMS_BROWSER_USERNAME='admin' FORMS_BROWSER_PASSWORD='...' ./scripts/verify-cross-browser.sh
```

`verify-debian-artifact.sh` باینری را داخل Debian تمیز، بدون شبکه، با filesystem فقط‌خواندنی و کاربر غیرریشه اجرا می‌کند. `verify-debian-install.sh` گردش نصب، migration، serve، health، backup، restore و doctor را با PostgreSQL هم‌نسخه اجرا می‌کند. `verify-systemd-runtime.sh` همین واحدها را با systemd واقعی به‌عنوان PID 1، PostgreSQL محلی، dispatch خودکار هر دو timer، restart و بوت دوبارهٔ container آزمایش می‌کند. reboot کامل kernel/VM و رسیدن timer به ساعت تقویمی مقرر همچنان کنترل نهایی میزبان مقصد است.

## سیاست migration

migrationها شماره‌دار، embedشده و forward-only هستند. باینری با schema عقب‌مانده serve نمی‌شود. ترتیب release عبارت است از backup معتبر، توقف سرویس، نصب artifact، `migrate status/up` و health check. rollback پس از migration با restore انجام می‌شود، نه downgrade SQL خودکار.

## معیار انتشار نهایی

- `make test`، integration PostgreSQL و race موفق؛
- `npm audit` و `govulncheck` بدون vulnerability قابل‌دسترسی؛
- checksum داخلی و بیرونی معتبر و SBOM قابل parse؛
- doctor، health و memory budget موفق؛
- restore drill روی محیط جدا با sample query برابر؛
- راهنمای نصب روی Debian تمیز اجرا و نتیجه در یادداشت همان release ثبت شده باشد.
