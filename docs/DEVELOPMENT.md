# توسعهٔ فرم‌ساز

## مرز نسخهٔ فعلی

نسخهٔ فعلی فرم‌ساز و انتشار عمومی، مرور و گزارش پاسخ، export/import پروژه،
دستیار دادهٔ فقط‌خواندنی، backup/restore کامل، سخت‌سازی سرویس، صفحهٔ سلامت و
بسته‌بندی release را پوشش می‌دهد. کنترل‌های لازم برای میزبان واقعی در راهنمای
release و نصب Debian آمده‌اند.

## toolchain و وابستگی‌ها

- Go toolchain: `go1.27.1`؛ نسخهٔ دقیق کتابخانه‌های Go در `go.mod` و `go.sum`
- Node.js: خط LTS 24؛ وابستگی‌های دقیق پنل در `web/admin/package-lock.json`
- PostgreSQL: 18 برای توسعه و CI؛ queryها فقط در repositoryهای محدود نوشته می‌شوند
- Vazirmatn: نسخهٔ `v33.003` با SHA-256 و مجوز OFL در `scripts/fetch-font.sh`

برای به‌روزرسانی فونت، version و checksum را فقط پس از بررسی release رسمی تغییر دهید و سپس `./scripts/fetch-font.sh` را اجرا کنید.

## گردش توسعه

```bash
make deps
make test
make build VERSION=0.4.0-dev COMMIT=local
./bin/forms version
```

`make build` ابتدا پنل production را در `internal/web/admin_dist` می‌سازد، سقف ۱۸۰ KiB gzip برای JS/CSS مدیریت و ۵۰ KiB gzip برای JavaScript عمومی را بررسی می‌کند و سپس باینری static با `CGO_ENABLED=0` می‌سازد. `BUILD_DATE` به‌صورت پیش‌فرض مقدار ثابت دارد تا ورودی یکسان خروجی تکرارپذیر بدهد؛ pipeline انتشار می‌تواند مقدار صریح UTC تزریق کند.

## تست PostgreSQL

```bash
docker compose -f compose.dev.yml up -d
export FORMS_TEST_DATABASE_URL='postgres://forms:forms-dev-only@127.0.0.1:5432/forms?sslmode=disable'
make integration
```

هر تست integration یک schema تصادفی و ایزوله می‌سازد. `TestStageOneWorkflow` قراردادهای هسته و نقش‌ها را نگه می‌دارد و آزمون‌های مرحلهٔ دوم مسیر کامل انتشار و پاسخ عمومی را حفظ می‌کنند. `TestStageThreeResponseBrowserAndReport` جست‌وجوی فارسی، گزارش، سه export، چاپ، بستهٔ پروژه و مرز امنیتی AI را روی PostgreSQL واقعی و provider ساختگی بررسی می‌کند. `TestStageThreeLargeDatasetBenchmark` به‌صورت deterministic صد هزار submission و دویست هزار answer می‌سازد، query گزارش را با `EXPLAIN (ANALYZE, BUFFERS)` اجرا و list/report/CSV streaming را اندازه می‌گیرد. بدون متغیر اتصال این آزمون‌ها skip می‌شوند؛ CI باید PostgreSQL واقعی داشته باشد.

## مرزهای معماری

- `cmd/forms`: parsing فرمان و اتصال lifecycle؛ منطق دامنه در این لایه قرار نمی‌گیرد.
- `internal/app`: ساخت dependencyها و lifecycle process.
- `internal/web`: HTTP، middleware، JSON، SSR و asset embed؛ SQL مستقیم ممنوع است.
- `internal/auth`, `internal/project`, `internal/form`: service و repository دامنه‌های مدیریت و schema/flow.
- `internal/publicform`: نشست عمومی، evaluator، پاسخ typed و pipeline فایل؛ تنها این لایه persistence فرم عمومی را مدیریت می‌کند.
- `internal/response`: read model پاسخ‌ها، aggregation محدود گزارش و exportهای streaming؛ label از snapshot همان نسخه خوانده می‌شود.
- `internal/projectpackage`: آرشیو نسخه‌دار پروژه با checksum، سقف decompression و remap شناسه‌های سطح پروژه؛ دادهٔ پاسخ‌دهنده در این مرز پذیرفته نمی‌شود.
- `internal/ai`: تنظیم provider، secret فایل خصوصی، SSRF guard، تاریخچهٔ شخصی و allowlist ابزارهای فقط‌خواندنی. این package SQL دلخواه از مدل دریافت نمی‌کند.
- `internal/backup` و `internal/restore`: آرشیو کامل، maintenance lease، checksum، staging، safety backup و rollback آفلاین.
- `internal/operations`: قفل process مشترک برای serve و انحصاری برای restore.
- `internal/database`: اتصال و migration عمومی.
- `internal/persiandate`: تبدیل و نمایش تقویم؛ persistence همچنان ISO/Gregorian است.
- `web/admin`: رابط Svelte؛ دسترسی داده فقط از client API مرکزی انجام می‌شود.

شناسهٔ صفحه، فیلد، گزینه و پایان پایدار و UUID است. ذخیرهٔ draft فقط با `expected_revision` انجام می‌شود؛ پاسخ 409 باید به‌عنوان تعارض واقعی مدیریت شود، نه retry کور.

## آزمون‌های مرورگر و systemd

بازبینی Firefox و WebKit داخل تصویر Playwright ثابت، بدون افزودن runtime به
بستهٔ production، با یک سرور آزمایشی قابل‌دسترسی از کانتینر اجرا می‌شود:

```bash
FORMS_BROWSER_USERNAME='admin' FORMS_BROWSER_PASSWORD='...' make cross-browser-check
```

این آزمون صفحه‌های اصلی desktop، سه نمای mobile، فرم عمومی، overflow افقی،
خطاهای runtime و تخلف‌های serious/critical از WCAG 2 A/AA را کنترل می‌کند.

برای تأیید عملی restart، بوت مجدد، timerها و jobهای backup/cleanup در یک Debian
13 ایزوله اجرا کنید:

```bash
make systemd-runtime-check
```

این آزمون PostgreSQL و systemd واقعی را داخل container دارای init اجرا می‌کند،
سرویس را restart می‌کند و سپس با restart خود container بازگشت خودکار سرویس و
timerها را می‌سنجد. این image نیز فقط ابزار آزمون است و وابستگی production نیست.

## کنترل پیش از تحویل

```bash
npm --prefix web/admin audit
make test
FORMS_TEST_DATABASE_URL='postgres://...' make integration
make build VERSION=0.4.0-dev COMMIT=local
FORMS_DATABASE_URL='postgres://...' ./scripts/measure-idle-memory.sh
./scripts/verify-debian-artifact.sh
make systemd-runtime-check
FORMS_BROWSER_USERNAME='admin' FORMS_BROWSER_PASSWORD='...' make cross-browser-check
FORMS_DEBIAN_IMAGE=debian:13-slim FORMS_POSTGRES_IMAGE=postgres:17-alpine ./scripts/verify-debian-install.sh
FORMS_DEBIAN_IMAGE=debian:12-slim FORMS_POSTGRES_IMAGE=postgres:15-alpine ./scripts/verify-debian-install.sh
```

علاوه بر تست خودکار، ورود، focus، صفحه‌کلید، RTL، عرض موبایل و LCP فرم عمومی باید در مرورگر واقعی بازبینی شوند. فایل‌های تولیدی `internal/web/admin_dist` بخشی از باینری هستند و باید همراه تغییر frontend بازسازی شوند. جزئیات نصب production در `docs/INSTALL-DEBIAN.md` و روش تأیید انتشار در `docs/RELEASE-FA.md` ثبت شده‌اند.
