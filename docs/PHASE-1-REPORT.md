# گزارش خروج مرحلهٔ اول

تاریخ بررسی نهایی: ۱۵ شهریور ۱۴۰۵ (2026-09-06)

## دامنهٔ تحویل

هستهٔ Go، migration و repositoryهای PostgreSQL، امنیت حساب/نشست، سه نقش، پنل مدیریت RTL، مدیریت کاربران و پروژه/زباله‌دان، فرم‌ساز پایه، date picker شمسی، draft revisioned و conflict handling پیاده‌سازی شده‌اند. قابلیت‌های مرحلهٔ دوم عمداً غیرفعال و با برچسب «مرحلهٔ بعد» نمایش داده می‌شوند.

## بودجه‌ها و شواهد

- بودجهٔ JS+CSS پنل: حداکثر ۱۸۰ KiB gzip؛ بررسی خودکار در `scripts/check-bundle.sh` و CI.
- خروجی اندازه‌گیری 2026-09-06: مجموع دقیق JS و CSS برابر 42,652 bytes gzip؛ JS برابر 35.51 kB و CSS برابر 7.72 kB در گزارش Vite.
- PostgreSQL integration: گردش کامل روی PostgreSQL 18.6، شامل ساخت اتمیک نخستین مدیر در دو درخواست هم‌زمان، سه نقش، ابطال نشست، تعارض revision و enforce شدن نقش‌ها.
- حافظهٔ idle باینری نهایی پس از آماده‌شدن health check: در سه اجرای مستقل بین ۱۴٬۴۸۰ تا ۱۶٬۵۶۴ KiB، با میانهٔ ۱۵٬۲۶۴ KiB (حدود ۱۵ MiB) و پایین‌تر از هدف ۸۰ MB؛ اندازه‌گیری با `scripts/measure-idle-memory.sh`.
- باینری نهایی: ELF استاتیک و stripped با اندازهٔ 11,759,740 bytes؛ دو build متوالی با ورودی یکسان SHA-256 برابر `1fa4fa04ca72ebc80f93f910cb987e91dfe223983f853a4b113d8bb71a2bb02b` تولید کردند.
- کیفیت و امنیت: ۱۰ آزمون رابط، آزمون‌های واحد و یکپارچهٔ Go، `go vet` و race detector پاس شدند؛ `npm audit` صفر آسیب‌پذیری و `govulncheck` صفر آسیب‌پذیری قابل‌دسترسی در کد یا بسته‌های importشده گزارش کردند.

## فرمان‌های پذیرش

```bash
make test
FORMS_TEST_DATABASE_URL='postgres://...' make integration
make build VERSION=0.1.0-stage1
./bin/forms version
```

مرحلهٔ دوم تا زمان تأیید این خروجی شروع نشده است.
