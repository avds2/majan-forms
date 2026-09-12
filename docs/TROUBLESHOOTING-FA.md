# عیب‌یابی و runbook

از این ترتیب شروع کنید؛ خروجی‌ها فاقد پاسخ و secret‌اند و برای گزارش حادثه مناسب‌ترند:

```bash
systemctl --no-pager --full status forms.service
journalctl -u forms.service -n 150 --no-pager
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms doctor'
curl -i http://127.0.0.1:8080/health/live
curl -i http://127.0.0.1:8080/health/ready
```

## سرویس بالا نمی‌آید

- `migrations`: فرمان `forms migrate status` و سپس `forms migrate up` را با همان کاربر و environment سرویس اجرا کنید.
- `process lock`: process قدیمی را با `systemctl status` پیدا کنید؛ فایل lock را کورکورانه حذف نکنید، lock با بسته‌شدن process آزاد می‌شود.
- `restore pending`: فایل `/var/lib/forms/restore-request.json` را حذف یا ویرایش نکنید. ابتدا `forms restore verify <archive>`، فضای دیسک و خروجی `pg_restore` را بررسی کنید.
- اگر بازیابی پنل شکست بخورد، واحد با exit code `75` متوقف می‌ماند و خودکار وارد حلقه نمی‌شود. پس از رفع علت فقط یک‌بار `systemctl start forms.service` را اجرا کنید.
- Argon2id timeout: فشار CPU میزبان را کم کنید؛ افزایش `FORMS_PASSWORD_HASH_LIMIT` فقط پس از اندازه‌گیری و با مقدار معقول انجام شود.

## health آماده نیست

- `database`: وضعیت PostgreSQL، peer/TCP auth و `FORMS_DATABASE_URL` را بررسی کنید.
- `migrations`: نسخهٔ باینری و schema برابر نیست.
- `storage`: مالکیت و mode مسیرهای data/tmp درست نیست یا دیسک read-only/full شده است.
- `maintenance`: backup یا عملیات نگه‌داری فعال است. تا پایان lease صبر و journal سرویس backup را بررسی کنید.

## دیسک پر است

```bash
df -h /var/lib/forms /var/backups/forms
du -sh /var/lib/forms/uploads /var/backups/forms
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms backup list'
```

تنها با `forms backup prune --keep N` بکاپ‌های عادی را حذف کنید. این فرمان آرشیو خراب، فایل ناشناس و safety backup را دست نمی‌زند. فایل upload را مستقیم حذف نکنید؛ رابطهٔ آن در PostgreSQL نگه‌داری می‌شود.

## بکاپ یا restore شکست می‌خورد

- وجود `pg_dump` و `pg_restore` و سازگاری نسخهٔ آن‌ها با server را بررسی کنید.
- پوشهٔ backup باید `0700` و خارج از data directory باشد.
- فایل `.partial` خروجی کامل نیست و برای restore استفاده نمی‌شود.
- هنگام restore مستقیم، `forms.service` باید متوقف باشد؛ در غیر این صورت قفل process عملیات را رد می‌کند.
- در شکست restore، safety backup با پیشوند `forms-safety-` را نگه دارید و از تکرار کورکورانه خودداری کنید.

## proxy و ورود

- برنامه باید روی loopback بماند و TLS در Caddy/Nginx پایان یابد.
- فقط CIDR واقعی proxy را در `FORMS_TRUSTED_PROXY_CIDRS` قرار دهید.
- در production، `FORMS_INSECURE_COOKIE` نباید وجود داشته باشد.
- خطای 403 cross-origin معمولاً از Host/Origin ناهماهنگ یا proxy header نادرست است؛ CORS عمومی فعال نکنید.

## upload

- `client_max_body_size` یا `request_body max_size` proxy باید کمی بیشتر از `FORMS_MAX_UPLOAD_BYTES` باشد.
- `forms cleanup` staging رهاشده و session منقضی را پاک می‌کند؛ نتیجه را در journal timer ببینید.
- فایل نهایی خارج web root است و فقط از endpoint احراز‌شده دانلود می‌شود.

## هوش مصنوعی

- ابتدا «آزمون اتصال» provider را اجرا کنید؛ صرف ذخیرهٔ URL و کلید کافی نیست.
- 401/403 معمولاً کلید یا مدل، 429 محدودیت provider، و timeout/502 ناسازگاری endpoint یا شبکه است.
- کلید در log/DB دیده نمی‌شود. وجود و mode فایل `/var/lib/forms/secrets/ai-provider.key` را بدون چاپ محتوای آن بررسی کنید.
- برای provider داخلی فقط allow-private صریح و endpoint محدود شبکه را فعال کنید؛ SSRF guard را دور نزنید.

## جمع‌آوری شواهد حادثه

زمان، نسخهٔ باینری، request ID، وضعیت health/doctor، فضای دیسک و ۱۵۰ خط journal را ثبت کنید. پاسخ کاربر، cookie، API key، رمز DB و محتوای upload را در ticket یا log کپی نکنید.
