# بکاپ و بازیابی

بکاپ کامل سامانه با قالب `forms-backup-v1.tar.gz` ساخته می‌شود. هر آرشیو شامل dump سفارشی PostgreSQL، فایل‌های نهایی پاسخ‌دهندگان، تنظیمات غیرمحرمانه و manifest دارای اندازه و SHA-256 همهٔ اجزاست. کلید API، رمز پایگاه‌داده، Turnstile secret و فایل تنظیمات محیطی عمداً داخل آرشیو قرار نمی‌گیرند.

ابزارهای `pg_dump` و `pg_restore` باید دقیقاً از همان major version سرور PostgreSQL باشند. `forms doctor` و `forms backup create` این شرط را کنترل می‌کنند تا آرشیوی که ظاهراً سالم است ولی روی سرور قابل بازیابی نیست ساخته نشود. پوشهٔ بکاپ باید خارج از `FORMS_DATA_DIR`، متعلق به کاربر `forms` و با دسترسی `0700` باشد:

```bash
sudo install -d -o forms -g forms -m 0700 /var/backups/forms
echo 'FORMS_BACKUP_DIR=/var/backups/forms' | sudo tee -a /etc/forms/forms.env >/dev/null
```

## ساخت، بررسی و نگه‌داری

مدیر می‌تواند در صفحهٔ «سلامت سیستم» تاریخچه و اعتبار آرشیوها را ببیند، ساخت
بکاپ تازه را آغاز کند، فقط فایل‌های معتبر را دریافت کند و برای یکی از آن‌ها
درخواست بازیابی ثبت کند. ساخت در پس‌زمینه انجام می‌شود و صفحه تا پایان وضعیت را
تازه می‌کند. پنل پس از تأیید متنی فقط درخواست اتمیک را ثبت و وب را کنترل‌شده
متوقف می‌کند؛ تغییر پایگاه‌داده و فایل‌ها هرگز داخل درخواست HTTP اجرا نمی‌شود و
در `ExecStartPre` آفلاین انجام می‌شود. فرمان‌های CLI نیز برای اتوماسیون در دسترس‌اند.

```bash
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms backup create'
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms backup list'
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms backup verify /var/backups/forms/forms-YYYYMMDDTHHMMSSZ.tar.gz'
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms backup prune --keep 14'
```

ساخت بکاپ ابتدا فضای آزاد را کنترل می‌کند، یک lease نگهداری می‌گیرد، ورود write جدید را با HTTP 503 و `Retry-After` متوقف می‌کند و تا پایان writeهای از قبل شروع‌شده منتظر می‌ماند. درخواست‌های خواندنی ادامه دارند. dump و فایل‌ها به آرشیو موقت نوشته می‌شوند، فایل نهایی به‌شکل اتمیک rename می‌شود و همان فایل بلافاصله با checksum و `pg_restore --list` دوباره بررسی می‌شود. فایل ناقص یا آرشیو خراب در `backup list` معتبر گزارش نمی‌شود و `prune` آن را حذف نمی‌کند.

timer پیشنهادی روزانه:

```bash
sudo install -m 0644 deploy/systemd/forms-backup.service /etc/systemd/system/
sudo install -m 0644 deploy/systemd/forms-backup.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now forms-backup.timer
systemctl list-timers forms-backup.timer
```

صرف وجود فایل بکاپ کافی نیست. به‌صورت دوره‌ای checksum را بررسی و restore drill را روی دیتابیس و data directory جدا اجرا کنید.

## پیش‌بررسی بازیابی

این فرمان هیچ داده‌ای را تغییر نمی‌دهد:

```bash
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms restore verify /var/backups/forms/forms-YYYYMMDDTHHMMSSZ.tar.gz'
```

بکاپی که schema آن از باینری نصب‌شده جدیدتر باشد قابل اعمال نیست. ابتدا باینری سازگار را نصب کنید.

## بازیابی مستقیم و آفلاین

بازیابی فقط وقتی مجاز است که process وب متوقف باشد. قفل فایل `process.lock` از اجرای اشتباه restore هم‌زمان با سرویس جلوگیری می‌کند.

```bash
sudo systemctl stop forms.service
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms restore apply --confirm /var/backups/forms/forms-YYYYMMDDTHHMMSSZ.tar.gz'
sudo systemctl start forms.service
curl --fail http://127.0.0.1:8080/health/ready
```

پیش از هر restore یک بکاپ ایمنی با پیشوند `forms-safety-` ساخته می‌شود. database restore در transaction واحد انجام می‌شود؛ فایل‌های نهایی فقط پس از موفقیت dump جابه‌جا می‌شوند. اگر migration، health پایگاه‌داده یا ثبت audit پس از بازیابی شکست بخورد، dump و فایل‌های بکاپ ایمنی بازگردانده می‌شوند. بکاپ‌های ایمنی عمداً توسط `backup prune` حذف نمی‌شوند و باید پس از تأیید دستی نگه‌داری یا حذف شوند.

## handoff با systemd

برای کم‌کردن زمان خطای انسانی، ابتدا درخواست اتمیک restore را آماده و سپس سرویس را restart کنید:

```bash
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms restore prepare /var/backups/forms/forms-YYYYMMDDTHHMMSSZ.tar.gz'
sudo systemctl restart forms.service
sudo journalctl -u forms.service -n 100 --no-pager
```

`ExecStartPre=forms restore pending` درخواست را پیش از بالا آمدن وب اجرا می‌کند و تنها پس از موفقیت، `restore-request.json` را حذف می‌کند. در شکست، سرویس بالا نمی‌آید و درخواست برای بررسی بعدی باقی می‌ماند. برای مشاهدهٔ دستی وضعیت بدون درخواست، اجرای `forms restore pending` خروجی `pending:false` و exit code صفر می‌دهد.

در واحد همراه محصول، شکست `restore pending` با exit code برابر `75` از restart
خودکار مستثنا شده است تا درخواست خراب باعث حلقهٔ بازیابی نشود. پس از رفع علت،
`systemctl start forms.service` را یک‌بار اجرا کنید. هم‌زمان فقط یک درخواست در
انتظار پذیرفته می‌شود؛ تکرار همان درخواست idempotent و انتخاب آرشیو دیگر رد است.

## restore drill روی محیط جدا

یک دیتابیس و data directory جدا بسازید، متغیرهای محیطی را به آن‌ها اشاره دهید، `migrate up` را اجرا کنید و سپس `restore apply --confirm` را روی همان محیط بزنید. در پایان حداقل این موارد را با مبدأ مقایسه کنید:

- شمار پروژه‌ها، نسخه‌ها، پاسخ‌ها و فایل‌های نهایی؛
- بازشدن یک فرم عمومی و جزئیات یک پاسخ؛
- `forms doctor` و `/health/ready`؛
- وجود audit با action برابر `restore.applied`؛
- checksum چند فایل نمونه.

دیتابیس آزمایشی را فقط پس از ثبت نتیجه و اطمینان از اتصال به نام صحیح حذف کنید.

## سناریوی خرابی

- اگر `backup create` شکست خورد، فایل `.partial` معتبر محسوب نمی‌شود؛ علت را از stderr/journal رفع و دوباره اجرا کنید.
- اگر lease به‌علت kill شدن process آزاد نشد، حداکثر پس از دو دقیقه منقضی می‌شود. پیش از دست‌کاری مستقیم جدول، `forms doctor` و log را بررسی کنید.
- اگر `restore pending` شکست خورد، سرویس را مکرراً restart نکنید. ابتدا archive، فضای دیسک، دسترسی پوشه‌ها و خروجی `pg_restore` را بررسی کنید.
- safety backup را تا پایان health check، sample query و بازبینی فایل‌ها حذف نکنید.
