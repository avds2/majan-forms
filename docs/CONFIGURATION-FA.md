# پیکربندی سامانه

پیکربندی از متغیرهای محیطی یا یک فایل JSON خوانده می‌شود. اگر `FORMS_CONFIG_FILE` تعیین شده باشد ابتدا همان فایل و سپس overrideهای محیطی اعمال می‌شوند. کلید ناشناخته یا JSON اضافه پس از شیء اصلی startup را متوقف می‌کند.

| متغیر | پیش‌فرض | توضیح و اثر امنیتی |
|---|---:|---|
| `FORMS_DATABASE_URL` | الزامی | URL اتصال PostgreSQL؛ بهتر است در Debian از Unix socket و peer auth استفاده شود. |
| `FORMS_HTTP_ADDR` | `127.0.0.1:8080` | نشانی listen؛ بازکردن روی interface عمومی باید آگاهانه و پشت TLS باشد. |
| `FORMS_DATA_DIR` | `/var/lib/forms` | uploads، فایل lock، secretها و staging؛ دسترسی `0700`. |
| `FORMS_BACKUP_DIR` | `/var/backups/forms` | بکاپ‌های کامل؛ باید مطلق، خارج از data directory و `0700` باشد. |
| `FORMS_TIMEZONE` | `Asia/Tehran` | نمایش تاریخ و مرز روز گزارش؛ ذخیرهٔ پایگاه‌داده همچنان UTC است. |
| `FORMS_LOG_LEVEL` | `info` | یکی از `debug/info/warn/error`؛ پاسخ، token و secret log نمی‌شوند. |
| `FORMS_DB_MAX_CONNS` | `6` | حداکثر connection pool، بین ۱ تا ۳۲. |
| `FORMS_DB_MIN_CONNS` | `0` | connection آماده؛ نباید از max بیشتر باشد. |
| `FORMS_SESSION_IDLE` | `12h` | انقضای نشست پس از بی‌کاری. |
| `FORMS_SESSION_ABSOLUTE` | `168h` | عمر مطلق نشست و بزرگ‌تر از idle. |
| `FORMS_SHUTDOWN_TIMEOUT` | `15s` | مهلت graceful shutdown. |
| `FORMS_PASSWORD_HASH_LIMIT` | `2s` | سقف benchmark Argon2id هنگام startup؛ کمتر از `100ms` رد می‌شود. |
| `FORMS_MAX_UPLOAD_BYTES` | `104857600` | سقف هر upload، بین ۱ MiB و ۱ GiB؛ proxy باید کمی بزرگ‌تر باشد. |
| `FORMS_TRUSTED_PROXY_CIDRS` | خالی | CIDRهای proxy مورد اعتماد برای `X-Forwarded-For`؛ معمولاً loopback. |
| `FORMS_TURNSTILE_SITE_KEY` | خالی | کلید عمومی Cloudflare Turnstile؛ باید همراه secret تنظیم شود. |
| `FORMS_TURNSTILE_SECRET` | خالی | secret Turnstile؛ در فایل محیطی با دسترسی محدود نگه‌داری شود. |
| `FORMS_INSECURE_COOKIE` | `false` | فقط توسعهٔ HTTP محلی؛ در production تنظیم نشود. |

نمونهٔ production:

```ini
FORMS_DATABASE_URL=postgres:///forms?host=/var/run/postgresql&sslmode=disable
FORMS_HTTP_ADDR=127.0.0.1:8080
FORMS_DATA_DIR=/var/lib/forms
FORMS_BACKUP_DIR=/var/backups/forms
FORMS_TIMEZONE=Asia/Tehran
FORMS_LOG_LEVEL=info
FORMS_DB_MAX_CONNS=6
FORMS_DB_MIN_CONNS=0
FORMS_SESSION_IDLE=12h
FORMS_SESSION_ABSOLUTE=168h
FORMS_SHUTDOWN_TIMEOUT=15s
FORMS_PASSWORD_HASH_LIMIT=2s
FORMS_MAX_UPLOAD_BYTES=104857600
FORMS_TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128
```

فایل `/etc/forms/forms.env` را با مالک `root:forms` و mode برابر `0640` نگه دارید. قرار دادن quote، substitution شل یا فاصله در مقدارها توصیه نمی‌شود؛ systemd فایل را با قواعد `EnvironmentFile` می‌خواند.

کلید provider هوش مصنوعی در این فایل یا PostgreSQL ذخیره نمی‌شود. پنل آن را اتمیک در `FORMS_DATA_DIR/secrets/ai-provider.key` با mode برابر `0600` می‌نویسد. بکاپ عادی نیز این secret را حمل نمی‌کند.

پس از تغییر پیکربندی:

```bash
sudo systemctl restart forms.service
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms doctor'
curl --fail http://127.0.0.1:8080/health/ready
```
