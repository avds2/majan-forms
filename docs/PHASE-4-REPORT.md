# گزارش پذیرش مرحلهٔ چهارم

آخرین اجرای تکمیلی: ۲۱ شهریور ۱۴۰۵ (2026-09-12)

## خروجی پیاده‌سازی

- آرشیو کامل `forms-backup-v1` با manifest، SHA-256، dump سفارشی PostgreSQL،
  فایل‌های نهایی و تنظیمات غیرمحرمانه؛ ساخت اتمیک، verify و retention.
- maintenance lease با ادامهٔ read، توقف موقت write و drain درخواست‌های در حال
  انجام؛ restore آفلاین با process lock، staging، safety backup، rollback و audit.
- صفحهٔ فقط‌مدیر «سلامت سیستم» با وضعیت DB/schema/disk/maintenance، آخرین بکاپ،
  تاریخچه، ساخت غیرهم‌زمان، دریافت آرشیو معتبر و بازیابی تأییدشده از طریق handoff
  آفلاین. ثبت درخواست اتمیک و idempotent است و شکست آفلاین وارد restart loop نمی‌شود.
- unitهای hardened برای سرویس، cleanup و backup timer، همراه نمونهٔ Caddy و
  Nginx، فرمان `doctor` و runbookهای فارسی.
- بستهٔ release تکرارپذیر با checksum داخلی و بیرونی، مجوزها و CycloneDX 1.5
  SBOM؛ امضای minisign در صورت ارائهٔ کلید release runner.

## شواهد اجرایی

| کنترل | نتیجه |
|---|---|
| نصب تمیز Debian 13 + PostgreSQL 17 | موفق؛ ۸ migration، serve/ready، backup، verify، restore و doctor |
| نصب تمیز Debian 12 + PostgreSQL 15 | موفق؛ همان گردش کامل |
| restore drill روی DB جدا | ۱ پروژه، ۲ پاسخ و ۱ کاربر پیش و پس از restore برابر |
| ارتقای schema | ارتقای نسخهٔ ۷ به ۸ و حفظ کاربر موفق |
| دادهٔ بزرگ | ۱۰۰هزار submission و ۲۰۰هزار answer؛ list حدود ۶۱ms، report حدود ۴۳۶ms، CSV حدود ۳.۸s در اجرای تکمیلی |
| حافظهٔ idle | ۲۰٬۷۶۰ KiB، حدود ۲۱ MiB |
| بودجهٔ frontend | پنل ۱۵۶٬۷۴۲ از ۱۸۴٬۳۲۰ بایت gzip؛ JS عمومی ۱٬۲۰۲ از ۵۱٬۲۰۰ بایت |
| dependency security | npm audit صفر؛ govulncheck صفر آسیب‌پذیری قابل‌دسترسی |
| race و integration | موفق |
| handoff بازیابی پنل | تأیید عبارت، درخواست `0600`، خروج کنترل‌شده و اجرای آفلاین موفق |
| runtime واقعی systemd | Debian 13 با PID 1؛ dispatch خودکار timerهای backup/cleanup، restart سرویس، بازیابی برنامه و فعال‌ماندن timerها پس از restart container موفق |
| Firefox و WebKit | همهٔ مسیرهای پنل، سه viewport موبایل، فرم عمومی، overflow، خطاهای runtime و تخلف‌های جدی/بحرانی WCAG A/AA موفق |

آزمون نصب یک ناسازگاری واقعی را آشکار کرد: `pg_dump` نسخهٔ ۱۷ از سرور ۱۵
آرشیوی تولید می‌کرد که همان سرور ۱۵ به‌علت گزینهٔ جدید قادر به restore آن نبود.
محصول اکنون برابری major نسخهٔ server، `pg_dump` و `pg_restore` را پیش از backup
کنترل می‌کند و `doctor` نیز آن را گزارش می‌دهد.

## کنترل میزبان پیش از انتشار پایدار

بستهٔ فعلی release candidate است. افزون بر syntax، lifecycle واحدها با systemd
واقعی به‌عنوان PID 1 در Debian 13 کنترل شده است: dispatch خودکار timerها، restart سرویس و
بوت دوبارهٔ container موفق بود. بااین‌حال restart یک container جای reboot کامل
kernel/VM مقصد و رسیدن timer به ساعت تقویمی واقعی را نمی‌گیرد. این دو کنترل باید
پیش از برچسب پایدار روی میزبان/VM مقصد تکمیل شوند.
