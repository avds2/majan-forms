# گزارش خروج مرحلهٔ سوم

تاریخ بررسی: ۱۸ شهریور ۱۴۰۵ (2026-09-09)

## دامنهٔ تحویل

چرخهٔ بهره‌برداری از داده تکمیل شد: جدول و جزئیات پاسخ بر پایهٔ snapshot نسخه، جست‌وجوی نرمال‌شدهٔ فارسی، فیلتر و صفحه‌بندی، حذف نرم و بازیابی، گزارش‌های PostgreSQL و نمودار lazy، گزارش چاپی HTML/SVG، خروجی‌های CSV/XLSX/JSON، بستهٔ انتقال پروژه و گفت‌وگوی AI فقط‌خواندنی. زیرساخت جداگانه، صف، Redis، runtime مرورگر یا SDK مدل به سرویس production افزوده نشد.

## شواهد معیارهای خروج

- بستهٔ `project-package-v1.zip` با manifest، checksum هر عضو و checksum کل، metadata، draft و نسخه‌های انتخابی ساخته می‌شود. round-trip پیش‌نویس و نسخه برابر معنایی بود، شناسهٔ پروژه/نسخه remap شد و پاسخ یا upload وارد بسته نشد.
- parser ورود، path traversal و zip-slip، عضو تکراری، zip bomb/نسبت فشرده‌سازی، checksum غلط، format/schema ناشناخته و UUID تکراری را پیش از ساخت پروژه رد می‌کند.
- CSV دارای UTF-8 BOM و محافظ formula injection است؛ XLSX جهت RTL و نوع عدد واقعی دارد؛ JSON نسخه‌دار مقدار ISO و نمایش شمسی را کنار UUID/label نسخه نگه می‌دارد.
- گزارش روی snapshot هر نسخه، KPI، روند روزانه، پایان/فعالیت صفحه، انتخاب‌ها و عددها شامل کمینه، میانه، میانگین، بیشینه و NPS را تجمیع می‌کند. فیلتر مشترک نسخه، بازهٔ شمسی، پایان و پاسخ فیلد در API، پنل و نسخهٔ چاپی برقرار است؛ فیلتر محتوایی فقط بر پاسخ‌های نهایی اعمال می‌شود و رابط، آمار ترافیکِ غیرقابل‌تفکیک را صریحاً از آن جدا می‌کند. route چاپ از HTML semantic، SVG و CSS A4 استفاده می‌کند و روی سرور به Chromium وابسته نیست.
- اتصال AI هر دو قرارداد Responses و Chat Completions را با streaming و function tool آزمون می‌کند. تنظیم تازه تا موفقیت compatibility test «آماده» محسوب نمی‌شود و تغییر تنظیم آزمون قبلی را باطل می‌کند.
- کلید provider فقط در فایل اتمیک `0600` زیر data directory است. HTTPS پیش‌فرض، DNS دوباره در زمان dial، رد IP خصوصی/link-local/metadata، منع redirect و اجازهٔ صریح loopback جلوی SSRF را می‌گیرد.
- allowlist مدل فقط خلاصهٔ schema، شمارش، گروه‌بندی choice، خلاصهٔ عددی، روند و filter summary است. ابزار نمونهٔ متن تنها با رضایت مستقل پروژه ظاهر می‌شود؛ SQL، filesystem، export و network tool وجود ندارد. حداکثر سه دور و چهار tool call در هر دور پذیرفته می‌شود.
- آزمون provider ساختگی ثابت کرد پروژهٔ خاموش صفر درخواست خارجی می‌سازد، حالت aggregate-only مقدار متنی واقعی پاسخ‌دهنده را به درخواست مدل نمی‌برد و نام ابزار ناشناخته همان دور رد می‌شود. تاریخچه متعلق به کاربر است و حذف گفت‌وگو پیام‌ها را cascade می‌کند.

## کارایی اندازه‌گیری‌شده

روی PostgreSQL محلی و dataset deterministic شامل `100,000` submission و `200,000` answer:

- فهرست ۲۵ پاسخ با شمارش کل: `29.99 ms`؛
- گزارش کامل: `412.34 ms`؛
- آماده‌سازی export: `46.78 ms`؛
- streaming کامل CSV صد هزار ردیفی به sink محدود: `4.80 s`؛
- aggregation choice با `EXPLAIN (ANALYZE, BUFFERS)`: `270.42 ms` و `5,092` shared hit block.

اعداد snapshot همین میزبان‌اند و آزمون threshold محافظه‌کارانهٔ list زیر ۲ ثانیه، report زیر ۵ ثانیه و CSV زیر ۳۰ ثانیه را در CI enforce می‌کند. generator از `generate_series` و UUIDهای deterministic استفاده می‌کند و schema آزمایش پس از اجرا حذف می‌شود.

مجموع همهٔ JS/CSS پنل، شامل chunkهای lazy مسیر، نمودار، پاسخ، تنظیمات و گفت‌وگو، `152,722` بایت gzip از سقف `184,320` بایت است. chunk گفت‌وگو `3.87 kB` gzip است. JavaScript عمومی بدون تغییر `1,202` بایت gzip باقی ماند.

## کیفیت و فرمان‌های پذیرش

```bash
make test
FORMS_TEST_DATABASE_URL='postgres://...' make integration
make build VERSION=0.3.0-stage3 COMMIT=local
npm --prefix web/admin audit
govulncheck ./...
```

تست integration مرحلهٔ سوم PostgreSQL واقعی، provider SSE ساختگی و dataset صد هزار ردیفی را پوشش می‌دهد. unit testهای جدا برای SSRF، secret اتمیک، هر دو قالب stream، archive و ورودی‌های مخرب وجود دارد. پنل در viewport موبایل `390×844` با جهت RTL و `scrollWidth = innerWidth = 390` بررسی شد و console خطا/هشدار نداشت. گزارش در Chromium و Firefox headless خوانا رندر شد؛ خروجی print واقعی Chromium یک صفحه با اندازهٔ `594.96×841.92 pt` (A4) بود.

`go test -race ./...` و `go vet` پاس شدند، `npm audit` صفر آسیب‌پذیری گزارش کرد و `govulncheck v1.7.0` هیچ آسیب‌پذیری قابل‌دسترسی در کد یا packageهای importشده نیافت.

## مرز مرحلهٔ بعد

backup/restore کامل، maintenance handoff، timer نگه‌داری backup، release archive، SBOM، hardening نهایی systemd و نصب/restore drill روی Debian تمیز در مرحلهٔ چهارم انجام می‌شود.
