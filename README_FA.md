# فرم‌ساز فارسی

[English](README.md)

سرویس فرم آنلاین فارسی و self-hosted برای یک سازمان است. پنل مدیریت Svelte،
فرم عمومی server-rendered و سرور HTTP در یک باینری static Go قرار می‌گیرند.
PostgreSQL تنها سرویس runtime اجباری است و Go و Node.js فقط هنگام build لازم‌اند.

وضعیت فعلی پروژه release candidate است. پیش از استقرار production، کنترل‌های
راهنمای انتشار و استقرار را کامل اجرا کنید.

## امکانات

- رابط مدیریت و فرم عمومی کاملاً فارسی و RTL، فونت وزیرمتن و نمایش تاریخ شمسی؛
  timestampها در PostgreSQL به‌صورت UTC نگه‌داری می‌شوند.
- ورود Argon2id و نشست سمت سرور، بدون ثبت‌نام عمومی، با نقش‌های سراسری مدیر،
  طراح فرم و مشاهده‌گر.
- draft دارای revision، تعداد ۳۴ نوع فیلد/بلوک، جابه‌جایی دسترس‌پذیر،
  پیش‌نمایش desktop/mobile و سه پوستهٔ رسمی، گرم و مینیمال.
- شرط‌های type-safe، assignment، اعتبارسنجی graph، آزمون مسیر و fallback قطعی.
- snapshot انتشار تغییرناپذیر، انتشار idempotent و توقف/ادامهٔ دریافت پاسخ.
- فرم عمومی چندصفحه‌ای بدون الزام JavaScript با progressive enhancement، نشست
  قابل‌ادامه، بازگشت به صفحهٔ قبل و حذف پاسخ‌هایی که پس از تغییر مسیر نامعتبرند.
- آپلود streaming با staging/finalize/cleanup، امضای ترسیمی با جایگزین PNG
  مناسب صفحه‌کلید، rate limit، honeypot، حداقل زمان تکمیل و Turnstile اختیاری.
- پاسخ typed و server-authoritative، جست‌وجو و فیلتر، حذف نرم، گزارش چندنسخه‌ای،
  چاپ A4 و خروجی CSV، XLSX و JSON.
- بستهٔ انتقال checksumدار برای draft و نسخه‌های منتخب، بدون دادهٔ پاسخ‌دهندگان.
- بکاپ کامل نسخه‌دار، verify و retention، restore آفلاین با safety backup و
  handoff امن systemd.
- health endpoint، صفحهٔ سلامت مدیر، فرمان `doctor`، cleanup و timerهای hardened.

## معماری

```text
مرورگر
  ├─ پنل Svelte ─────┐
  └─ فرم عمومی HTML ─┼─ باینری Go/net/http ── PostgreSQL
                     ├─ assetها و migrationهای embedشده
                     ├─ فایل‌ها و secretهای خصوصی
                     └─ provider اختیاری سازگار با OpenAI
```

فرم عمومی server-rendered و progressively enhanced است؛ پنل در زمان build
کامپایل و داخل باینری embed می‌شود. منطق دامنه و SQL در packageهای محدود قرار
دارند، نه handlerها. نسخهٔ منتشرشده تغییرناپذیر است و هر نشست عمومی تا پایان به
نسخه‌ای که با آن آغاز شده pin می‌ماند. جزئیات معماری و مرزهای پیاده‌سازی در
[راهنمای توسعه](docs/DEVELOPMENT.md) آمده است.

## شروع سریع توسعه

نیازمندی build شامل Go 1.27.1، Node.js 24 و npm، Docker Compose و ابزارهای
معمول Unix است. Docker در این مسیر فقط PostgreSQL را اجرا می‌کند.

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
  --name 'مدیر سامانه'
./bin/forms serve
```

فرمان ساخت مدیر رمز را امن و تعاملی می‌پرسد. در اتوماسیون از فایل دارای دسترسی
`0600` و گزینهٔ `--password-file` استفاده کنید؛ رمز را در آرگومان فرمان نگذارید.

پنل روی `http://127.0.0.1:8080` و readiness روی
`http://127.0.0.1:8080/health/ready` است. فرم منتشرشده در `/f/<slug>` دیده
می‌شود. `FORMS_INSECURE_COOKIE=true` فقط برای HTTP محلی است و نباید در
production تنظیم شود.

برای production از [راهنمای نصب از صفر Debian](docs/INSTALL-DEBIAN.md) و
[روش ساخت و تأیید release](docs/RELEASE-FA.md) استفاده کنید.

## دستیار دادهٔ AI اختیاری

AI پیش‌فرض خاموش است و در این وضعیت هیچ درخواست خارجی ساخته نمی‌شود. مدیر
می‌تواند endpoint سازگار با OpenAI Responses API یا Chat Completions را وارد
کند و باید آزمون داخلی streaming و ابزارها را پاس کند. هیچ مدل یا سرویس AI
همراه پروژه ارائه نمی‌شود و در دسترس‌نبودن provider مانع کار فرم‌ها نیست.

دستیار فقط‌خواندنی است: تنها ابزارهای aggregation محدود و پارامتری سمت سرور را
فراخوانی می‌کند و اجازهٔ SQL دلخواه، تغییر، حذف یا export دادهٔ خام ندارد. رضایت
برای هر پروژه جداست؛ نمونه‌خوانی متن آزاد رضایت دوم می‌خواهد و پیش‌فرض خاموش
است. کلید در فایل خصوصی `0600` زیر `FORMS_DATA_DIR` نگه‌داری و از PostgreSQL و
backup حذف می‌شود. endpoint و redirect نیز کنترل SSRF دارند.

## CLI مدیریتی

راهنما و نسخه بدون تنظیم DB هم کار می‌کنند:

```bash
./bin/forms --help
./bin/forms version
./bin/forms admin --help
./bin/forms backup --help
./bin/forms restore --help
```

فرمان‌ها شامل `serve`، `migrate status|up`،
`admin create|reset-password`، `backup create|verify|list|prune`،
`restore verify|prepare|apply|pending`، `cleanup`، `doctor` و `version` هستند.
restore عمداً آفلاین و نیازمند تأیید صریح است؛ گردش درست در
[راهنمای بکاپ و بازیابی](docs/BACKUP-RESTORE-FA.md) ثبت شده است.

## آزمون و تأیید release

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

کنترل‌های اختیاری دیگر، unitهای واقعی systemd در Debian ایزوله، نصب تمیز روی
Debian 12 و 13 و مسیرهای UI/دسترس‌پذیری Firefox و WebKit را می‌آزمایند. فرمان‌ها
و environment لازم در [راهنمای توسعه](docs/DEVELOPMENT.md) آمده است.

build سقف gzip برابر ۱۸۰ KiB برای مجموع JavaScript/CSS پنل و ۵۰ KiB برای
JavaScript فرم عمومی را enforce می‌کند. آرشیو release شامل `SHA256SUMS` داخلی،
checksum بیرونی، SBOM با قالب CycloneDX 1.5، unitهای استقرار، نمونهٔ reverse
proxy، مستندات، مجوز MIT پروژه و مجوز فونت است.

## مستندات

- [نصب Debian](docs/INSTALL-DEBIAN.md)
- [مرجع پیکربندی](docs/CONFIGURATION-FA.md)
- [راهنمای مدیر و خلاصهٔ CLI](docs/ADMIN-GUIDE-FA.md)
- [بکاپ و بازیابی](docs/BACKUP-RESTORE-FA.md)
- [ارتقا و rollback](docs/UPGRADE-FA.md)
- [عیب‌یابی](docs/TROUBLESHOOTING-FA.md)
- [مدل امنیت](docs/SECURITY.md)
- [توسعه و آزمون](docs/DEVELOPMENT.md)
- [ساخت release](docs/RELEASE-FA.md)

## مجوز

کد برنامه تحت [مجوز MIT](LICENSE) منتشر می‌شود. فونت وزیرمتن جداگانه تحت SIL
Open Font License قرار دارد و متن مجوز آن در
[assets/fonts/OFL.txt](assets/fonts/OFL.txt) موجود است.
