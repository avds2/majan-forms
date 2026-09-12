# نصب از صفر روی Debian

این راهنما برای یک میزبان Debian 13 تمیز نوشته شده است. خروجی production یک باینری static است؛ روی سرور به Go، Node.js، npm، Redis، PgBouncer یا سرویس صف نیاز نیست. تنها وابستگی اجباریِ بیرونی PostgreSQL است و TLS باید در reverse proxy موجودِ میزبان پایان یابد.

پیش از انتقال artifact، روی ماشین build اجرای آن را در Debian تمیز، بدون شبکه و با کاربر غیرریشه کنترل کنید:

```bash
make build VERSION=0.4.0-rc13 COMMIT='<commit>'
./scripts/verify-debian-artifact.sh
sha256sum bin/forms
```

این کنترل جای backup و آزمون restore محیط production را نمی‌گیرد، اما وابسته نبودن باینری به runtime یا کتابخانهٔ مشترک نصب‌نشده را ثابت می‌کند.

## ۱. آماده‌سازی سیستم و پایگاه‌داده

فرمان‌ها را با حسابی اجرا کنید که `sudo` دارد:

```bash
sudo apt update
sudo apt install --no-install-recommends adduser ca-certificates postgresql postgresql-client
sudo adduser --system --group --home /var/lib/forms --no-create-home forms
sudo -u postgres createuser --no-createdb --no-createrole --no-superuser forms
sudo -u postgres createdb --owner=forms forms
```

بستهٔ `postgresql-client` باید هم‌نسخهٔ اصلی سرور باشد؛ برای نمونه سرور ۱۵ با
`pg_dump` و `pg_restore` نسخهٔ ۱۵. فرمان `forms doctor` این سازگاری را کنترل
می‌کند و در صورت اختلاف، ساخت بکاپ نیز پیش از تولید فایل متوقف می‌شود.

این راهنما از اتصال Unix socket و احراز هویت peer استفاده می‌کند؛ بنابراین رمز پایگاه‌داده در فایل تنظیمات ذخیره نمی‌شود. اگر سیاست میزبان اتصال TCP را الزام می‌کند، یک role محدود و TLS معتبر بسازید و URL اتصال را متناسب با همان سیاست جایگزین کنید.

## ۲. نصب باینری و تنظیمات

فایل `forms` ساخته‌شده در CI یا روی ماشین build مورد اعتماد را به سرور منتقل کنید، checksum آن را با release تطبیق دهید و سپس:

```bash
sudo install -o root -g root -m 0755 forms /usr/local/bin/forms
sudo install -d -o root -g forms -m 0750 /etc/forms
sudo install -d -o forms -g forms -m 0700 /var/lib/forms
sudo install -d -o forms -g forms -m 0700 /var/backups/forms
sudoedit /etc/forms/forms.env
```

محتوای حداقلی `/etc/forms/forms.env`:

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
```

دسترسی فایل را محدود کنید:

```bash
sudo chown root:forms /etc/forms/forms.env
sudo chmod 0640 /etc/forms/forms.env
```

برای Turnstile اختیاری، هر دو متغیر `FORMS_TURNSTILE_SITE_KEY` و `FORMS_TURNSTILE_SECRET` را اضافه کنید. اگر یکی از آن‌ها تنظیم نشود، startup عمداً رد می‌شود. برای production متغیر `FORMS_INSECURE_COOKIE` را تنظیم نکنید. اگر Nginx روی همان میزبان به برنامه proxy می‌کند، `FORMS_TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128` را اضافه کنید؛ هدر `X-Forwarded-For` از هر مبدأ دیگری عمداً نادیده گرفته می‌شود.

## ۳. migration و مدیر نخست

ابتدا migrationها را با همان کاربر سرویس اجرا کنید:

```bash
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms migrate status'
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms migrate up'
```

رمز مدیر را موقتاً در فایلی با دسترسی `0600` قرار دهید و بعد از ساخت حساب آن را امن حذف کنید:

```bash
sudo install -o forms -g forms -m 0600 /dev/null /var/lib/forms/admin-password
sudoedit /var/lib/forms/admin-password
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms admin create \
  --email admin@example.ir --username admin --name "مدیر سامانه" \
  --password-file /var/lib/forms/admin-password'
sudo shred -u /var/lib/forms/admin-password
```

فایل environment را در خط فرمان expand نکنید؛ این کار می‌تواند مقدار محرمانهٔ
اتصال TCP را در فهرست processها آشکار کند. روش `sh -c` بالا فقط مسیر فایل را در
خط فرمان نگه می‌دارد و `set -a` انتساب‌های خوانده‌شده را برای process برنامه
export می‌کند. فایل نمونه باید صرفاً شامل انتساب‌های سادهٔ `KEY=value` باشد و
فقط root اجازهٔ ویرایش آن را داشته باشد.

## ۴. systemd و پاک‌سازی فایل‌های موقت

unitهای همراه پروژه را نصب و بررسی کنید:

```bash
sudo install -o root -g root -m 0644 deploy/systemd/forms.service /etc/systemd/system/forms.service
sudo install -o root -g root -m 0644 deploy/systemd/forms-cleanup.service /etc/systemd/system/forms-cleanup.service
sudo install -o root -g root -m 0644 deploy/systemd/forms-cleanup.timer /etc/systemd/system/forms-cleanup.timer
sudo install -o root -g root -m 0644 deploy/systemd/forms-backup.service /etc/systemd/system/forms-backup.service
sudo install -o root -g root -m 0644 deploy/systemd/forms-backup.timer /etc/systemd/system/forms-backup.timer
sudo systemd-analyze verify /etc/systemd/system/forms.service /etc/systemd/system/forms-cleanup.service /etc/systemd/system/forms-cleanup.timer /etc/systemd/system/forms-backup.service /etc/systemd/system/forms-backup.timer
sudo systemctl daemon-reload
sudo systemctl enable --now forms.service forms-cleanup.timer forms-backup.timer
```

پاک‌سازی ساعتی، نشست‌های عمومی منقضی و uploadهای staging رهاشده را حذف و finalize نیمه‌تمام را بازیابی می‌کند. اجرای دستی آن idempotent است:

```bash
sudo systemctl start forms-cleanup.service
sudo journalctl -u forms-cleanup.service -n 30 --no-pager
```

## ۵. TLS و reverse proxy

برنامه فقط روی loopback گوش می‌دهد. برای سرور تازه، Caddy ساده‌ترین مسیر است:

```bash
sudo apt install --no-install-recommends caddy
sudo install -o root -g root -m 0644 deploy/caddy/Caddyfile /etc/caddy/Caddyfile
sudoedit /etc/caddy/Caddyfile
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

دامنهٔ `forms.example.ir` را جایگزین کنید. Caddy گواهی TLS را مدیریت و درخواست را به loopback می‌فرستد.

اگر میزبان از قبل Nginx یا Caddy دارد، فایل اصلی آن را overwrite نکنید. فقط یک site/server block تازه از `deploy/nginx/forms.conf` یا بخش site نمونهٔ Caddy بسازید، ابتدا `nginx -t` یا `caddy validate` را اجرا و سپس reload کنید. `client_max_body_size` یا `request_body max_size` باید اندکی بیشتر از `FORMS_MAX_UPLOAD_BYTES` باشد.

نمونه‌ها IP واقعی را در `X-Forwarded-For` قرار می‌دهند؛ برنامه فقط وقتی آن را می‌پذیرد که نشانی مستقیم proxy در `FORMS_TRUSTED_PROXY_CIDRS` آمده باشد. این متغیر را به شبکهٔ عمومی یا CIDR گسترده تنظیم نکنید.

## ۶. کنترل پس از نصب

```bash
curl --fail http://127.0.0.1:8080/health/live
curl --fail http://127.0.0.1:8080/health/ready
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms migrate status'
systemctl --no-pager --full status forms.service forms-cleanup.timer forms-backup.timer
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms doctor'
```

`ready` تنها وقتی ۲۰۰ برمی‌گرداند که PostgreSQL، نسخهٔ migration و مسیرهای writable آماده باشند. سپس از طریق دامنهٔ HTTPS وارد پنل شوید، یک فرم آزمایشی منتشر کنید و یک پاسخ عمومی ثبت کنید.

## اتصال اختیاری دستیار داده

دستیار داده هیچ وابستگی تازه‌ای روی Debian نصب نمی‌کند و در حالت پیش‌فرض کاملاً خاموش است. مدیر از «تنظیمات ← اتصال provider» نشانی HTTPS، قرارداد Responses یا Chat Completions، نام مدل و کلید API را ذخیره می‌کند و سپس باید آزمون streaming و function tools را با موفقیت اجرا کند. کلید در `/var/lib/forms/secrets/ai-provider.key` با mode `0600` نگه‌داری می‌شود و در PostgreSQL، log یا پاسخ API قرار نمی‌گیرد.

هر پروژه رضایت جدا دارد. فعال‌سازی پایه فقط ساختار فرم و aggregationها را در اختیار provider می‌گذارد؛ گزینهٔ نمونه‌خوانی متن پاسخ‌دهنده مستقل، هشدار‌دار و پیش‌فرض آن خاموش است. endpoint خصوصی فقط برای provider داخلی و با تأیید صریح مدیر مجاز شود. redirect، userinfo، scheme نامعتبر و HTTP غیر-loopback حتی در این حالت رد می‌شوند.

نبود provider یا مدل مانع کار اصلی سامانه نیست. برنامه provider جایگزین انتخاب نمی‌کند و کلید فقط برای احراز هویت به endpoint اعتبارسنجی‌شده ارسال می‌شود؛ جزئیات در [سیاست امنیت و حریم داده](SECURITY.md) آمده است.

## ارتقا و بازگشت امن

پیش از ارتقا `forms backup create` و سپس `forms backup verify` را اجرا کنید. باینری تازه را نصب کنید، `forms migrate up` را اجرا کنید و سرویس را restart کنید. migrationها forward-only هستند؛ بازگشت صرفِ باینری پس از migration تضمین‌شده نیست، بنابراین rollback باید بر پایهٔ backup آزموده‌شده انجام شود. گردش کامل در [BACKUP-RESTORE-FA.md](BACKUP-RESTORE-FA.md) آمده است.
