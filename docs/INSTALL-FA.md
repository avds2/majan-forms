# نصب از صفر روی Debian

راهنمای اجرایی و آزموده‌شدهٔ نصب با همهٔ فرمان‌های PostgreSQL، کاربر سیستم،
پوشه‌ها، تنظیمات، migration، مدیر اولیه، Caddy/Nginx، systemd، timer و firewall
در فایل [INSTALL-DEBIAN.md](INSTALL-DEBIAN.md) نگه‌داری می‌شود. این فایل نام
قراردادی مستند نصب در بستهٔ release است و لینک بالا مرجع یگانه و بدون نسخهٔ
تکراری راهنماست.

این مسیر روی ترکیب‌های زیر از ابتدا تا health، backup و restore اجرا شده است:

- Debian 13 slim با PostgreSQL 17؛
- Debian 12 slim با PostgreSQL 15.

نسخهٔ اصلی `postgresql-client` باید با نسخهٔ اصلی سرور یکسان باشد. پیش از
راه‌اندازی production، `forms doctor` را اجرا کنید.
