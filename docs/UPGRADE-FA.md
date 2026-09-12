# ارتقا و بازگشت نسخه

migrationهای پایگاه‌داده forward-only هستند. بنابراین rollback امن بعد از اجرای migration با نصب دوبارهٔ باینری قدیمی انجام نمی‌شود؛ باید safety backup آزموده‌شده را برگردانید.

## پیش از ارتقا

```bash
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms doctor'
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms backup create'
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms backup list'
```

نام، SHA-256 و نتیجهٔ verify تازه‌ترین بکاپ را ثبت کنید. checksum آرشیو release را با فایل منتشرشده مقایسه و امضای minisign را در صورت ارائه بررسی کنید. سپس release را در پوشه‌ای موقت extract و `SHA256SUMS` داخلی آن را کنترل کنید.

## نصب نسخهٔ تازه

```bash
sha256sum --check forms-VERSION-linux-amd64.tar.gz.sha256
tar -xzf forms-VERSION-linux-amd64.tar.gz
cd forms-VERSION
sha256sum --check SHA256SUMS
sudo systemctl stop forms.service
sudo install -o root -g root -m 0755 bin/forms /usr/local/bin/forms
sudo install -o root -g root -m 0644 deploy/systemd/*.service deploy/systemd/*.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms migrate status'
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms migrate up'
sudo systemctl start forms.service
curl --fail http://127.0.0.1:8080/health/ready
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms doctor'
```

پس از health، ورود مدیر، بازشدن یک پروژه، فرم عمومی و ثبت یک پاسخ آزمایشی را بررسی کنید. تا پایان این کنترل safety backup و باینری release قبلی را نگه دارید.

## rollback

اگر migration اجرا نشده است، سرویس را متوقف و باینری قبلی را نصب کنید. اگر migration اجرا شده یا وضعیت آن نامعلوم است:

```bash
sudo systemctl stop forms.service
sudo install -o root -g root -m 0755 /path/to/compatible/forms /usr/local/bin/forms
sudo -u forms sh -c 'set -a; . /etc/forms/forms.env; set +a; exec /usr/local/bin/forms restore apply --confirm /var/backups/forms/forms-YYYYMMDDTHHMMSSZ.tar.gz'
sudo systemctl start forms.service
curl --fail http://127.0.0.1:8080/health/ready
```

باینری استفاده‌شده برای restore باید schema بکاپ را بشناسد. اگر بکاپ با نسخهٔ جدیدتری ساخته شده، ابتدا همان باینری جدید را برای restore اجرا کنید و پس از بازگشت داده، باینری سازگار مقصد را نصب کنید.

جزئیات safety backup و drill در [BACKUP-RESTORE-FA.md](BACKUP-RESTORE-FA.md) است.
