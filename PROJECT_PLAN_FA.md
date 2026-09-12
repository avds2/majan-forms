# برنامهٔ جامع طراحی و پیاده‌سازی سرویس فرم آنلاین فارسی

> وضعیت: برنامه‌ریزی فنی، بدون پیاده‌سازی  
> تاریخ تحقیق: ۵ سپتامبر ۲۰۲۶  
> هدف اجرا: چهار مرحلهٔ مستقل برای GPT-5.6 Sol Max  
> محیط هدف: یک سرور Debian با ۲ هستهٔ پردازنده و ۴ گیگابایت RAM که میزبان سرویس‌های دیگری نیز هست

## ۱. خلاصهٔ تصمیم نهایی

محصول یک فرم‌ساز فارسی، تک‌سازمانی و چندکاربره است. هر «پروژه» در نسخهٔ اول شامل یک فرم، پیش‌نویس آن، نسخه‌های منتشرشده، پاسخ‌ها، گزارش‌ها، فایل‌ها و گفت‌وگوی AI همان فرم است. ثبت‌نام عمومی وجود ندارد و مدیر کاربران داخلی را ایجاد می‌کند.

معماری پیشنهادی:

- یک باینری Go برای API، صفحات عمومی، احراز هویت، گزارش، export/import، backup/restore و اتصال AI.
- رابط مدیریت با Svelte و TypeScript که در زمان build به فایل‌های ایستا تبدیل و داخل باینری Go embed می‌شود؛ در سرور production به Node.js نیاز نیست.
- فرم عمومی با HTML سمت سرور و JavaScript بسیار کم برای تقویم شمسی و بهبود تعامل؛ مسیر اصلی فرم بدون وابستگی به SPA باقی می‌ماند.
- PostgreSQL برای داده‌های تراکنشی و گزارش‌گیری.
- فایل‌ها روی دیسک محلی و خارج از مسیر public web root.
- یک process اصلی؛ بدون Redis، Elasticsearch، message broker، object storage، worker جداگانه یا مرورگر headless.
- استقرار با systemd و reverse proxy موجود؛ Caddy انتخاب پیش‌فرض برای نصب تازه و Nginx گزینهٔ مستندشدهٔ دوم.

این انتخاب کمی از SQLite سنگین‌تر است، اما برای ثبت هم‌زمان پاسخ‌ها، نسخه‌بندی، فیلتر و گزارش، بازیابی مطمئن و توسعهٔ آینده ریسک بسیار کمتری دارد. روی سرور ۴ گیگابایتی، با pool کوچک و تنظیمات محافظه‌کارانه، PostgreSQL همچنان انتخاب مناسبی است.

## ۲. هدف‌ها و حدود محصول

### هدف‌های اصلی

- تجربهٔ کاملاً فارسی، راست‌به‌چپ، سریع، ساده و مناسب موبایل.
- ساخت فرم چندصفحه‌ای با drag-and-drop، تنظیمات واضح و پیش‌نمایش فوری.
- گردش شرطی بصری شبیه فلوچارت با چند پایان و تغییر کنترل‌شدهٔ مقدار فیلد.
- انتشار فرم با لینک عمومی و ثبت پاسخ ناشناس.
- پشتیبانی از مجموعهٔ وسیع فیلدها، فایل و اعتبارسنجی.
- نسخه‌بندی تغییرناپذیر فرم‌های منتشرشده.
- مشاهده، جست‌وجو، فیلتر، گزارش، چاپ و خروجی پاسخ‌ها.
- ورود و خروج پروژه به شکل بستهٔ قابل‌انتقال.
- بکاپ و بازیابی کامل سیستم.
- تحلیل و گفت‌وگو با داده‌های هر پروژه از طریق API سازگار با OpenAI، بدون اجازهٔ تغییر داده.
- نصب و نگهداری ساده روی Debian با کمترین وابستگی runtime.

### موارد خارج از نسخهٔ اول

- ثبت‌نام عمومی، چندسازمانی و صورت‌حساب.
- مجوزهای جداگانه برای هر پروژه؛ نقش‌ها در کل سازمان اعمال می‌شوند.
- همکاری هم‌زمان زنده در ویرایشگر. تداخل ویرایش با optimistic concurrency تشخیص داده می‌شود.
- ذخیره و ادامهٔ فرم در روز دیگر به‌عنوان قابلیت کاربری. وضعیت موقت صرفاً برای اجرای چندصفحه‌ای نگه‌داری و خودکار پاک می‌شود.
- ایمیل، webhook، پرداخت، اتصال CRM و گردش تأیید.
- کد JavaScript یا CSS سفارشی کاربر.
- منطق حلقه‌ای، اسکریپت دلخواه یا SQL تولیدشده توسط مدل AI.
- استخراج یا تحلیل محتوای فایل‌های پیوست توسط AI.
- تولید PDF روی سرور. گزارش چاپی با HTML/SVG و print stylesheet ساخته می‌شود و مرورگر می‌تواند آن را PDF ذخیره کند.
- آنتی‌ویروس اجباری روی سرور؛ اتصال اختیاری ClamAV می‌تواند بعداً اضافه شود.

## ۳. نتیجهٔ تحقیق از محصولات مشابه

موارد زیر باید به شکل ایده و الگو استفاده شوند، نه کپی ظاهری:

- **Tally:** گروه‌های شرطی `همه/هرکدام`، پرش بین صفحه‌ها، پایان اختصاصی و تنظیم سادهٔ منطق کنار محتوای فرم. مزیت آن زبان ساده و progressive disclosure است.
- **Typeform Logic Map:** نمایش تمام مسیرها در یک نقشه، zoom/pan، اتصال بصری و نشان‌دادن خطا روی گره. در محصول ما نقشه علاوه بر نمایش، محل اصلی ویرایش اتصال‌ها خواهد بود.
- **Jotform:** palette فیلد، drag-and-drop، شرط‌های دارای اولویت و تأکید بر preview سناریوهای مختلف. ترتیب اجرای شرط‌ها باید در UI کاملاً قابل‌مشاهده باشد.
- **Google Forms:** سادگی مدل صفحه/بخش و امکان پایان فرم از یک شاخه. از محدودیت آن که branching را فقط به چند نوع سؤال محدود می‌کند عبور می‌کنیم، ولی سادگی ذهنی آن حفظ می‌شود.

نتیجهٔ UX: ویرایشگر فرم و ویرایشگر جریان دو نمای جدا اما هماهنگ هستند. قرار دادن تمام منطق در پنل خصوصیات هر فیلد برای فرم‌های بزرگ مبهم می‌شود؛ در عوض، نمای «ساخت» برای محتوا و نمای «مسیرها» برای شاخه‌ها در نظر گرفته می‌شود.

## ۴. پشتهٔ فنی و تصمیم‌های معماری

| لایه | انتخاب | دلیل |
|---|---|---|
| Backend | Go، `net/http` استاندارد | مصرف کم، concurrency مناسب، باینری واحد، API استاندارد و وابستگی کم |
| Router | الگوهای routing خود `net/http` | حذف router ثالث و کافی بودن امکانات نسخه‌های جدید Go |
| PostgreSQL driver | `pgx/v5` و `pgxpool` | pure Go، سریع، پشتیبانی مستقیم از PostgreSQL و pool داخلی |
| SQL | SQL دست‌نویس در repositoryهای محدود | شفافیت، حذف ORM و کنترل دقیق queryها؛ همهٔ queryها باید integration test داشته باشند |
| Admin UI | Svelte + TypeScript + Vite | خروجی کامپایل‌شدهٔ کم‌حجم و مناسب UI تعاملی بدون Node runtime |
| Public UI | Go HTML templates + progressive enhancement | HTML اولیهٔ سریع، دسترس‌پذیری بهتر و JavaScript کمتر |
| Flow editor | `@xyflow/svelte`، lazy-loaded | drag، edge، pan، zoom و keyboard support آماده؛ فقط در صفحهٔ مسیرها دانلود شود |
| Chart | Chart.js، lazy-loaded | نمودارهای رایج با API پایدار؛ داده پیش‌تجمیع‌شده به آن داده می‌شود |
| Persian date (browser) | `Intl.DateTimeFormat` + `jalaali-js` | نمایش native و تبدیل/اعتبارسنجی سبک با zero dependency |
| Persian date (server) | `jalaali/go-jalaali` | الگوریتم هم‌خانواده با کتابخانهٔ مرورگر و تست‌پذیری تبدیل‌ها |
| XLSX | Excelize StreamWriter | خروجی تایپ‌شده و streaming برای جلوگیری از مصرف زیاد حافظه |
| Password | Argon2id از `golang.org/x/crypto` | انتخاب استاندارد برای hash رمز؛ پارامترها روی سرور واقعی benchmark می‌شوند |
| Assets | `go:embed` | CSS، JS، template و فونت در باینری release قرار می‌گیرند |
| Reverse proxy | Caddy پیش‌فرض، Nginx جایگزین | Caddy نصب و HTTPS خودکار ساده دارد؛ با reverse proxy موجود نیز سازگار می‌ماند |

### وابستگی‌های runtime روی سرور

فقط این موارد لازم‌اند:

1. PostgreSQL و ابزارهای client شامل `pg_dump` و `pg_restore`.
2. باینری خود برنامه.
3. systemd که در Debian موجود است.
4. یک reverse proxy؛ Caddy یا Nginx.

Node.js، Go compiler و browser فقط در محیط build/test لازم‌اند، نه روی سرور production.

### ساختار پیشنهادی مخزن

```text
cmd/forms/                 entry point و subcommandها
internal/app/              composition و lifecycle
internal/auth/             user, role, session, password
internal/project/          project و trash
internal/form/             draft, schema, publish, version
internal/flow/             validation و اجرای گردش شرطی
internal/publicform/       session موقت و submit عمومی
internal/submission/       response و answer
internal/storage/          فایل محلی و atomic operations
internal/report/           aggregation و printable report
internal/transfer/         import/export پروژه و پاسخ‌ها
internal/backup/           backup/restore و maintenance mode
internal/ai/               provider, tools, limits, streaming
internal/persiandate/      تبدیل، parse و format شمسی
internal/web/              handler، middleware و template
migrations/                migrationهای SQL شماره‌دار و embedشده
web/admin/                 Svelte admin application
web/public/                enhancement سبک فرم عمومی
web/shared/                design tokens و Persian date picker
assets/fonts/              Vazirmatn WOFF2 و مجوز
deploy/systemd/            service و timer
deploy/caddy/              نمونه Caddyfile
deploy/nginx/              نمونه server block
docs/                      مستندات فارسی
scripts/                   build/release/fetch-font و تست نصب
```

وابستگی میان packageهای domain یک‌طرفه باشد؛ handlerها فقط serviceهای application را صدا بزنند و SQL یا filesystem مستقیم در UI layer وجود نداشته باشد. interface فقط در مرزی تعریف شود که واقعاً چند پیاده‌سازی یا fake تستی لازم دارد.

## ۵. مدل دامنه و داده

### تعریف «پروژه»

در نسخهٔ اول هر پروژه دقیقاً یک فرم دارد. پروژه این اجزا را در خود جمع می‌کند:

- metadata و slug عمومی؛
- یک پیش‌نویس قابل‌ویرایش؛
- صفر یا چند نسخهٔ منتشرشدهٔ immutable؛
- پاسخ‌ها و فایل‌ها؛
- گزارش‌ها و گفت‌وگوهای AI؛
- theme، تنظیمات انتشار و تنظیمات ضداسپم.

نام table و serviceها نباید فرض «همیشه فقط یک فرم» را در تمام لایه‌ها hard-code کند. افزودن entity `forms` در آینده باید مهاجرتی افزایشی باشد، نه بازنویسی پاسخ‌ها.

### جدول‌های اصلی

- `users`: ایمیل/نام کاربری یکتا، نام نمایشی، role، password hash، وضعیت، زمان آخرین ورود.
- `sessions`: hash توکن تصادفی، user، زمان انقضا، آخرین فعالیت و metadata محدود.
- `projects`: عنوان، slug، status، active version، owner metadata و `deleted_at`.
- `form_drafts`: schema JSONB، revision، checksum، updated_by و updated_at.
- `form_versions`: version number، schema immutable، checksum، published_by و published_at.
- `public_sessions`: توکن hash‌شده، version، answers موقت، history صفحه‌ها، expires_at و وضعیت submit.
- `submissions`: project/version، شناسه عمومی، زمان شروع/ثبت، ending، fingerprint حداقلی ضدتکرار و `deleted_at`.
- `submission_answers`: field UUID، type، و ستون‌های typed برای text/number/bool/date/time/json.
- `stored_files`: submission یا public session، storage key تصادفی، نام اصلی، size، detected MIME، checksum و status.
- `analytics_daily`: شمارندهٔ روزانهٔ view/start/step/complete به تفکیک version و page؛ بدون ذخیرهٔ IP خام.
- `ai_conversations` و `ai_messages`: فقط برای پروژه‌ای که AI آن فعال است.
- `audit_log`: ورود، تغییر نقش، انتشار، export، حذف، backup و restore؛ بدون ذخیرهٔ پاسخ یا secret.
- `app_settings`: تنظیمات غیرمحرمانه و schema version.

### انتخاب JSONB و دادهٔ typed

تعریف فرم و graph به صورت JSONB versioned ذخیره می‌شود چون schema میان پروژه‌ها متفاوت است. پاسخ‌ها صرفاً یک JSON بزرگ نیستند؛ هر پاسخ در `submission_answers` با نوع مناسب نگه‌داری می‌شود تا sort/filter/aggregate درست و indexپذیر باشد. در صورت نیاز یک snapshot JSON برای سرعت نمایش جزئیات نیز می‌تواند در `submissions` قرار گیرد، اما منبع حقیقت answerهای typed است.

### شناسه‌ها و پایداری

- project، version، page، field، option، rule، ending و submission شناسهٔ UUID دارند.
- جابه‌جایی یا تغییر عنوان فیلد، UUID را عوض نمی‌کند.
- duplicate کردن فیلد UUID تازه می‌سازد.
- نسخهٔ فرم title/labelهای همان زمان را نگه می‌دارد تا پاسخ قدیمی معنای خود را از دست ندهد.
- گزارش چندنسخه‌ای با UUID پایدار field تجمیع می‌کند؛ فیلد حذف‌شده با label نسخهٔ خودش نمایش داده می‌شود.

### migration

- migrationها forward-only و داخل باینری embed شوند.
- شروع سرویس در صورت عقب‌بودن schema به‌طور پیش‌فرض fail شود؛ migration صریح با `forms migrate up` اجرا گردد.
- هر release قبل از migration دیتابیس، backup سازگار می‌گیرد.
- migration مخرب یا حذف ستون در همان release معرفی نشود؛ الگوی expand/migrate/contract رعایت شود.

## ۶. schema فرم و فیلدها

schema منتشرشده باید مستقل از state ویرایشگر باشد. مختصات گراف و وضعیت پنل‌ها در `editor_metadata` بماند و runtime فقط تعریف معنایی را بخواند.

هر فیلد حداقل دارای این خصوصیات است:

- `id`, `type`, `label`, `help_text`, `required`؛
- مقدار پیش‌فرض و placeholder در صورت مرتبط بودن؛
- قواعد validation با پیام خطای فارسی قابل‌ویرایش؛
- تنظیمات نمایش، عرض و visibility؛
- گزینه‌ها با UUID پایدار برای choice fieldها؛
- تنظیمات گزارش‌پذیری و محرمانگی AI؛
- تنظیمات مخصوص type مانند min/max، precision، پسوند فایل یا scale.

### فیلدهای ورودی نسخهٔ اول

**متن و شناسه**

- متن کوتاه، متن بلند، ایمیل، تلفن، URL؛
- نام کامل و آدرس به شکل field مرکب؛
- کد ملی/شناسه با validator آماده اما قابل‌غیرفعال‌کردن؛
- الگوی سفارشی محدود با regex سمت سرور و guard در برابر regex پرهزینه.

**عدد و سنجش**

- عدد صحیح/اعشاری، مبلغ، درصد؛
- rating ستاره‌ای، scale عددی و NPS؛
- فیلد محاسباتی/مخفی برای جریان و تغییر مقدار.

**انتخاب**

- بله/خیر، radio، checkbox چندانتخابی، dropdown تک/چندانتخابی؛
- matrix/grid با محدودیت تعداد سطر و ستون؛
- امکان مرتب‌سازی گزینه‌ها و گزینهٔ «سایر».

**تاریخ و زمان**

- تاریخ شمسی، ساعت، تاریخ‌وساعت شمسی و بازهٔ تاریخ شمسی؛
- محدودیت ابتدا/انتها، روزهای مجاز و غیرفعال؛
- ورودی و date picker همیشه شمسی، ذخیره‌سازی استاندارد در PostgreSQL.

**فایل و تأیید**

- آپلود یک یا چند فایل؛
- امضا با canvas و خروجی PNG؛
- رضایت/پذیرش قوانین با label و متن الزام.

**بلوک‌های نمایشی**

- عنوان، متن توضیحی ساده، divider، تصویر و فاصله؛
- متن arbitrary HTML پذیرفته نشود. لینک به‌عنوان دادهٔ ساختاریافته و escapeشده تعریف شود.

### محدودیت‌های منطقی فیلد

- سقف جهانی upload در تنظیمات سرور؛ پیش‌فرض پیشنهادی ۱۰۰MB برای هر فایل.
- سقف پروژه و فیلد نمی‌تواند از سقف جهانی بیشتر باشد.
- مدیر فرم می‌تواند حجم، تعداد و پسوند مجاز هر upload field را کمتر کند.
- پسوندهای executable، HTML و SVG همیشه مسدود باشند؛ allowlist امن سراسری بر انتخاب مدیر اولویت دارد.
- فرم بیش از حد بزرگ با soft warning و سقف سخت قابل‌تنظیم کنترل شود؛ پیشنهاد آغازین ۲۰۰ صفحه، ۱۰۰۰ فیلد و ۵۰۰ rule.

## ۷. نسخه‌بندی، ویرایش و انتشار

### چرخهٔ وضعیت

`پیش‌نویس → منتشرشده → بسته → زباله‌دان → حذف دائم`

- ویرایش همواره روی draft انجام می‌شود.
- Publish یک snapshot immutable با checksum می‌سازد و pointer نسخهٔ فعال را atomically تغییر می‌دهد.
- نشست عمومی که با نسخهٔ قدیمی شروع شده تا پایان به همان نسخه pinned می‌ماند.
- بستن پروژه لینک را حفظ می‌کند ولی صفحهٔ «پذیرش پاسخ متوقف است» نشان می‌دهد.
- بازنشر slug را عوض نمی‌کند.

### autosave و تداخل

- تغییر draft با debounce کوتاه ذخیره شود.
- هر write شامل revision مورد انتظار است.
- اختلاف revision با خطای conflict، نمایش نویسنده/زمان آخرین تغییر و گزینهٔ reload یا ذخیرهٔ copy پاسخ داده شود.
- merge خودکار پیچیده و live collaboration در نسخهٔ اول انجام نشود.

### validation قبل از انتشار

انتشار باید در یک transaction و فقط پس از این بررسی‌ها ممکن باشد:

- عنوان و slug معتبر و یکتا؛
- حداقل یک صفحه و یک پایان؛
- reference نشکسته به field/page/option؛
- نداشتن cycle در graph؛
- وجود مسیر پیش‌فرض از هر صفحهٔ غیرپایانی؛
- دسترسی‌پذیری حداقل یک ending از start؛
- نبود صفحه یا ending غیرقابل‌دسترسی، یا تأیید آگاهانه برای warningهای غیرمسدودکننده؛
- type compatibility شرط‌ها و assignmentها؛
- معتبر بودن محدودیت فایل و تاریخ؛
- تست schema serialization و محاسبهٔ checksum.

## ۸. موتور گردش شرطی

### مدل graph

- nodeها: `start`, `page`, `ending`.
- edgeها: مبدا، مقصد، priority، شرط اختیاری و actionها.
- هر page می‌تواند چند edge شرطی مرتب و دقیقاً یک fallback داشته باشد.
- اولین شرط true بر اساس priority اجرا می‌شود؛ در نبود آن fallback انتخاب می‌شود.
- graph باید DAG باشد. دکمهٔ «قبلی» از history نشست استفاده می‌کند و edge برگشتی نمی‌سازد.

### شرط‌ها

- گروه‌های recursive با `all` و `any`؛ UI در ابتدا یک سطح ساده نشان می‌دهد و nesting با «گروه شرط» باز می‌شود.
- عملگرها بر اساس type محدود شوند: برابر/نابرابر، شامل، خالی/پر، بزرگ‌تر/کوچک‌تر/بین، قبل/بعد تاریخ، شامل هر/همه گزینه‌ها.
- مقایسهٔ متن پس از normalization حروف فارسی و ارقام انجام شود؛ مقدار اصلی دست‌نخورده نگه داشته شود.
- شرط روی فیلدی که در مسیر فعلی هنوز پاسخ داده نشده `unknown` است و true تلقی نمی‌شود.

### action تغییر مقدار

- target می‌تواند hidden/calculated field یا فیلد صفحهٔ بعد باشد، نه فیلد صفحهٔ گذشته.
- source می‌تواند مقدار ثابت، کپی یک answer سازگار یا عملیات عددی محدود باشد.
- حالت target: `جایگزینی` یا `تنظیم فقط اگر خالی است`.
- اگر فیلد برای پاسخ‌دهنده قابل‌ویرایش است، UI مشخص کند مقدار از منطق آمده است؛ تنظیم «قفل مقدار» اختیاری باشد.
- script، expression آزاد و eval ممنوع است.

### اجرای server-authoritative

- مرورگر answers صفحه را به endpoint مرحله می‌فرستد.
- سرور validation، assignment و انتخاب edge را انجام می‌دهد و صفحهٔ بعد را برمی‌گرداند.
- اگر پاسخ قبلی عوض شود، مسیر از ابتدا دوباره محاسبه و answerهای مربوط به صفحه‌های دیگرِ غیرقابل‌دسترسی حذف می‌شوند.
- submit نهایی کل مسیر را دوباره ارزیابی می‌کند؛ client نمی‌تواند field پنهان یا ending را جعل کند.
- evaluator تابعی deterministic باشد و fixtureهای مشترک، سناریوهای branch را پوشش دهند.

### UX فلوچارت

- canvas دارای pan/zoom/fit، minimap اختیاری، keyboard navigation و drag handle باشد.
- page node خلاصهٔ عنوان و تعداد فیلدها را نشان دهد؛ ending رنگ و شکل متمایز داشته باشد.
- edge label خلاصهٔ فارسی شرط را نشان دهد؛ fallback با «در غیر این صورت» مشخص شود.
- ساخت edge پنل rule builder را باز کند؛ شرط پیچیده مستقیماً روی خط تایپ نشود.
- cycle، reference شکسته، نبود fallback و unreachable node با badge و پنل خطا نمایش داده شود.
- «آزمایش مسیر» اجازه دهد جواب نمونه وارد و مسیر highlight شود، بدون ساخت submission.

## ۹. سیاست سراسری تقویم شمسی و فارسی

اصل: تقویم در UI شمسی است، نه در storage.

- timestampها در PostgreSQL با `timestamptz` و UTC ذخیره شوند.
- تاریخ بدون ساعت با نوع `date` و معادل Gregorian ذخیره شود تا تغییر timezone روز را جابه‌جا نکند.
- timezone نمایشی پیش‌فرض `Asia/Tehran` و از تنظیمات سازمان قابل‌تغییر باشد.
- تمام date pickerها، فیلتر پاسخ، زمان انتشار، گزارش، audit، trash و backup history شمسی نمایش داده شوند.
- ورودی `۱۴۰۵/۰۶/۱۴` و `1405/06/14` هر دو پذیرفته و normalize شوند.
- UI نام ماه‌های فارسی، شروع هفته از شنبه و جهت کلیدهای مناسب RTL داشته باشد.
- API داخلی تاریخ را ISO 8601 استاندارد منتقل کند؛ تنها componentهای presentation آن را شمسی کنند.
- export JSON تاریخ استاندارد ISO و یک فیلد نمایشی شمسی داشته باشد؛ CSV/XLSX می‌توانند ستون شمسیِ خوانا و ستون ISO اختیاری ارائه کنند.

### تست‌های اجباری تقویم

- نوروز، پایان اسفند در سال کبیسه و غیرکبیسه؛
- عبور بازه از پایان سال؛
- timestampهای نزدیک نیمه‌شب تهران و UTC؛
- DST تاریخی ایران و نبود تغییر ساعت در سال‌های جدید؛
- ارقام فارسی/عربی/لاتین و جداکننده‌های مختلف؛
- round-trip تبدیل browser/server برای یک fixture مشترک؛
- sort/filter با مقدار ذخیره‌شده، نه رشتهٔ نمایشی شمسی.

## ۱۰. طراحی رابط و تجربهٔ کاربری

### زبان بصری

- Vazirmatn با WOFF2 از release رسمی در build دانلود، نسخه pin و SHA-256 بررسی شود؛ فایل و مجوز داخل artifact قرار گیرند و font از همان origin سرو شود.
- `font-display: swap`، preload فقط وزن اصلی و subset/weight محدود برای جلوگیری از دانلود اضافی.
- design token برای رنگ، فاصله، radius، shadow، typography و حالت focus.
- آیکون‌ها SVG داخلی و محدود؛ component library بزرگ اضافه نشود.
- متن‌های UI فقط فارسی و لحن کوتاه و روشن داشته باشند؛ اصطلاح فنی در tooltip توضیح داده شود.

### ناوبری مدیریت

صفحهٔ پروژه این tabها را دارد:

1. نمای کلی
2. ساخت فرم
3. مسیرها
4. انتشار
5. پاسخ‌ها
6. گزارش‌ها
7. گفت‌وگو با داده
8. تنظیمات

ویژگی‌های مشترک:

- breadcrumb کوتاه، command/search داخلی و autosave status واضح؛
- empty state همراه با اقدام اصلی، نه صفحهٔ خالی؛
- destructive actionها با نام دقیق target و امکان بازیابی؛
- toast تنها برای پیام گذرا؛ خطای قابل‌اقدام نزدیک component بماند؛
- skeleton محدود و بدون animation آزاردهنده؛ احترام به `prefers-reduced-motion`.

### ویرایشگر فرم

- sidebar راست: صفحه‌ها، ترتیب و افزودن/حذف صفحه.
- canvas مرکز: preview واقعی صفحه و drop zoneهای مشخص.
- sidebar چپ: palette فیلد یا inspector فیلد انتخاب‌شده.
- drag-and-drop با دکمه‌های «بالا/پایین» و keyboard equivalent همراه باشد.
- تغییر type فقط وقتی دادهٔ تنظیمات قابل‌حفظ است؛ در غیر این صورت warning دقیق بدهد.
- preview desktop/mobile و preview مسیر با دادهٔ نمونه.
- builder روی desktop/tablet کامل است؛ روی موبایل مدیریت پاسخ‌ها ممکن ولی ویرایش graph فقط با پیام پیشنهاد استفاده از صفحهٔ بزرگ‌تر.

### فرم عمومی

- عرض خوانا، تمرکز روی یک صفحه در هر لحظه، progress واضح و دکمه‌های «قبلی/بعدی» ثابت اما غیرمزاحم.
- validation هم inline و هم summary بالای صفحه؛ focus به اولین خطا منتقل شود.
- رنگ به‌تنهایی حامل خطا یا وضعیت نباشد.
- پاسخ‌ها هنگام رفت‌وبرگشت همان نشست حفظ شوند.
- double-click روی submit پاسخ تکراری نسازد.
- قطع شبکه پیام retry بدون از دست‌رفتن state همان صفحه بدهد.
- سه theme آماده: «مینیمال روشن»، «رسمی» و «گرم»؛ تنظیم رنگ اصلی، لوگو، تصویر پس‌زمینه و alignment.

### دسترس‌پذیری

- هدف WCAG 2.2 AA برای مسیر عمومی و عملیات اصلی مدیریت.
- label واقعی برای تمام controlها، error text توصیفی، focus visible و ترتیب tab منطقی.
- target لمسی حداقل مناسب، contrast آزمایش‌شده و zoom تا ۲۰۰٪ بدون از دست‌رفتن عملیات.
- ARIA فقط جایی استفاده شود که HTML semantic کافی نیست؛ graph و drag keyboard fallback داشته باشند.

## ۱۱. اجرای فرم عمومی، نشست موقت و فایل

### public session

- نخستین POST یک توکن تصادفی با entropy بالا می‌سازد؛ فقط hash آن در DB ذخیره و خود token در hidden field منتقل می‌شود.
- session به form version pinned است و ۲۴ ساعت پس از آخرین فعالیت منقضی می‌شود.
- این session قابلیت «ادامه در روز دیگر» را در UI ارائه نمی‌کند و پس از submit یا expiry پاک‌سازی می‌شود.
- task پاک‌سازی با subcommand و systemd timer اجرا می‌شود.

### upload pipeline

1. request body پیش از parse با limit سخت بسته شود.
2. multipart به‌شکل stream خوانده شود، نه بارگذاری کامل در RAM.
3. نام storage کاملاً تصادفی و مستقل از نام کاربر باشد.
4. فایل ابتدا در staging همان filesystem نوشته، size و checksum و MIME magic بررسی شود.
5. extension و MIME با allowlist فیلد و allowlist سراسری تطبیق داده شود.
6. پس از commit submission با rename اتمیک به محل نهایی منتقل شود.
7. فایل abandoned با timer پاک شود.
8. download فقط پس از مجوز، با `Content-Disposition: attachment` و `nosniff` انجام شود.

فایل‌ها هرگز از یک static directory عمومی سرو نشوند. نام اصلی فقط metadata است و در path استفاده نمی‌شود. ظرفیت آزاد دیسک در پنل سیستم نمایش داده و زیر threshold امن، upload جدید متوقف شود.

## ۱۲. کاربران، نقش‌ها و امنیت نشست

### نقش‌ها

- **مدیر:** کاربران، تنظیمات، تمام پروژه‌ها، backup/restore و حذف دائم.
- **طراح فرم:** ساخت/ویرایش/انتشار پروژه، پاسخ‌ها، گزارش و export؛ بدون مدیریت کاربر یا restore.
- **مشاهده و خروجی:** مشاهدهٔ پروژه، پاسخ و گزارش و گرفتن export؛ بدون تغییر فرم.

همهٔ کاربران داخلی به همهٔ پروژه‌ها بر اساس نقش سراسری دسترسی دارند. project-level ACL در نسخهٔ اول وجود ندارد.

### حساب و نشست

- اولین مدیر با `forms admin create` ساخته شود.
- ایجاد کاربر دیگر فقط توسط مدیر؛ ثبت‌نام عمومی و بازیابی ایمیلی وجود ندارد.
- reset مدیر قفل‌شده با subcommand امن CLI ممکن باشد.
- رمز با Argon2id و salt تصادفی hash شود؛ پارامتر OWASP حداقل baseline است و startup benchmark سقف زمانی مناسب را بررسی کند.
- session token تصادفی، opaque و hash‌شده در DB؛ rotation پس از login و تغییر رمز.
- cookie با نام `__Host-...`، ویژگی‌های Secure، HttpOnly، SameSite=Lax و Path=/.
- idle timeout و absolute timeout جدا؛ پیشنهاد آغازین ۱۲ ساعت و ۷ روز.
- logout و تغییر رمز sessionهای لازم را revoke کند.

## ۱۳. پاسخ‌ها، جست‌وجو، گزارش و چاپ

### پاسخ‌ها

- جدول پاسخ‌ها با column chooser، sort، pagination سروری و filter builder ساده.
- جست‌وجوی normalization‌شده برای ی/ک عربی و فارسی و ارقام.
- جزئیات پاسخ بر اساس labelهای همان form version.
- حذف تکی/گروهی ابتدا soft delete و سپس purge سی‌روزه؛ audit اجباری.
- فایل‌ها از صفحهٔ جزئیات و با کنترل دسترسی دانلود شوند.

### گزارش‌های آماده

- تعداد view/start/complete و نرخ تکمیل؛
- روند پاسخ‌ها با بازهٔ شمسی؛
- پایان‌های مختلف و drop-off صفحه‌ها؛
- توزیع گزینه‌ها، میانگین/میانه/min/max عدد، NPS و rating؛
- خلاصهٔ matrix، تاریخ‌ها و تعداد فایل؛
- فهرست متن‌های آزاد با جست‌وجو، بدون word cloud در نسخهٔ اول؛
- filter مشترک بر نسخه، زمان، ending و پاسخ فیلدهای منتخب.

درخواست گزارش، aggregation را در PostgreSQL انجام دهد و فقط دادهٔ نهایی کوچک را به browser بفرستد. chartهای بزرگ server-side bucket شوند.

### گزارش چاپی

- route مستقل HTML با header پروژه، زمان/فیلتر شمسی، KPIها، جدول‌ها و نمودارهای SVG.
- CSS مخصوص A4، کنترل page break، رنگ قابل‌چاپ و footer شماره صفحه تا حد پشتیبانی browser.
- هیچ Chromium/Puppeteer روی سرور نصب نشود.
- دکمهٔ «چاپ / ذخیره به‌صورت PDF» از print dialog مرورگر استفاده کند.
- canvasهای Chart.js برای چاپ مبنا نباشند؛ نسخهٔ print از SVG یا table semantic استفاده کند.

## ۱۴. خروجی و ورودی

### خروجی پاسخ‌ها

- CSV UTF-8 با BOM اختیاری برای سازگاری Excel و محافظت در برابر CSV formula injection.
- XLSX با type واقعی number/date/bool و StreamWriter.
- JSON نسخه‌بندی‌شده با field UUID، label نسخه، مقدار ISO و مقدار نمایشی شمسی.
- export بر اساس filter فعال یا تمام پاسخ‌ها؛ فایل با streaming response یا temporary file محدود تولید شود.
- export بزرگ timeout مرورگر را دور نزند؛ در نسخهٔ اول سقف معقول و پیام راهنمای CLI، نه queue جداگانه.

### بستهٔ پروژه

فرمت `project-package-v1.zip` شامل موارد زیر است:

- `manifest.json` با format version، app version و checksumها؛
- metadata، draft، versionهای انتخابی، flow و theme؛
- logo/background و assetهای خود فرم؛
- بدون پاسخ و فایل پاسخ‌دهندگان.

ورود پروژه باید zip-slip، zip bomb، path traversal، duplicate ID، schema ناسازگار و checksum غلط را رد کند. project/slug جدید ساخته و شناسه‌های سطح پروژه remap شوند، اما روابط داخلی field/page/rule حفظ گردند. preview تغییرات قبل از import نمایش داده شود.

## ۱۵. بکاپ، بازیابی و زباله‌دان

### بستهٔ بکاپ کامل

فرمت `forms-backup-v1.tar.gz`:

- `manifest.json`: app/schema version، تاریخ، hostname اختیاری، اندازه و checksum همه اجزا؛
- `database.dump`: خروجی custom format از `pg_dump`؛
- `uploads/`: فایل‌های referenced؛
- `settings.json`: تنظیمات غیرمحرمانه؛
- secrets فقط با انتخاب صریح مدیر و warning در بستهٔ جدا یا محافظت‌شده قرار گیرند؛ پیش‌فرض شامل API key نباشد.

### consistency

- backup کامل maintenance lock کوتاه‌مدت می‌گیرد: readها ادامه دارند، write/submit با پیام retry موقت متوقف می‌شوند.
- ابتدا state و نسخه ثبت، سپس dump و فایل‌ها snapshot شوند و در پایان checksum تأیید گردد.
- archive به‌شکل stream ساخته شود و فضای خالی قبل از شروع بررسی شود.
- فایل ناقص با suffix موقت بماند و به‌عنوان backup معتبر فهرست نشود.

### زمان‌بندی

- `forms backup create`, `forms backup verify`, `forms backup list` و policy حذف نسخه‌های قدیمی.
- systemd timer روش پیشنهادی Debian؛ نمونه cron نیز در مستندات.
- محل backup به‌صورت پیش‌فرض خارج از uploads و با permission 0700/0600.
- پنل فقط history/status و ایجاد/download backup را ارائه کند.

### restore از پنل

restore عملیات عادی داخل request وب نیست:

1. مدیر archive را upload یا انتخاب می‌کند.
2. برنامه آن را در staging با limit بررسی و manifest/checksum/schema compatibility را validate می‌کند.
3. UI پیش‌نمایش و هشدار قطع write نشان می‌دهد و تأیید دوباره می‌گیرد.
4. برنامه `restore-request.json` اتمیک می‌نویسد و با exit code مشخص خارج می‌شود.
5. systemd با `ExecStartPre=forms restore-pending` بازیابی را خارج از process وب اجرا می‌کند.
6. restore پیش از دست‌کاری، safety backup می‌گیرد؛ DB و فایل‌ها در staging بازسازی و سپس swap می‌شوند.
7. سرویس بالا می‌آید، health check و schema check اجرا و نتیجه audit می‌شود.
8. در شکست، safety backup rollback می‌شود یا سرویس در maintenance با دستور بازیابی روشن باقی می‌ماند.

restore از CLI نیز باید همان validation و state machine را استفاده کند. تست عملی restore، نه صرف ساخته‌شدن archive، معیار سلامت backup است.

## ۱۶. اتصال و گفت‌وگوی AI

### تنظیمات provider

- endpoint پایه، API mode، model، timeout، max output tokens و API key.
- دو mode: `Responses API` و `Chat Completions compatible`؛ چون بسیاری از providerهای سازگار فقط دومی را کامل پیاده می‌کنند.
- `Responses API` انتخاب پیشنهادی برای endpointهایی است که آن را پشتیبانی می‌کنند؛ compatibility test در پنل قابلیت streaming و tools را آزمایش می‌کند.
- برای portability از `net/http` و DTOهای کوچک داخلی استفاده شود، نه وابستگی کامل SDK و نه URL hard-coded.
- API key در فایل secret با permission 0600 زیر data directory نگه‌داری، هرگز دوباره در UI نمایش یا log نشود.
- HTTP client مشترک با timeout، سقف response، redirect policy و connection reuse.

### دسترسی داده

- AI برای هر پروژه جداگانه و پیش‌فرض خاموش است.
- حالت پیش‌فرض فقط aggregateها را می‌فرستد.
- ارسال نمونهٔ متن آزاد با انتخاب صریح کاربر، سقف تعداد/طول و نمایش هشدار دادهٔ شخصی.
- فایل پیوست و secret هیچ‌گاه ارسال نشود.
- model هیچ SQL یا filesystem access ندارد.

### ابزارهای read-only مدل

- `get_form_schema_summary`
- `count_submissions`
- `group_by_choice`
- `numeric_summary`
- `time_series`
- `filter_summary`
- `sample_text_answers` با cap و فقط در حالت مجاز

پارامتر هر tool schema محدود دارد، server مجوز و field type را validate می‌کند و query از templateهای امن ساخته می‌شود. تعداد دورهای tool calling، تعداد ردیف، زمان query و token budget محدود است. نتیجه می‌تواند Markdown امن و یک chart specification declarative محدود تولید کند؛ chart spec پیش از render validate می‌شود.

### ایمنی و UX

- متن پاسخ‌دهنده دادهٔ غیرقابل‌اعتماد است و نباید دستور مدل تلقی شود.
- AI کاملاً read-only است و ابزار تغییر/حذف/export خام ندارد.
- پاسخ شامل توضیح «بر اساس چه فیلتر و چه تعداد پاسخ» باشد.
- برای سؤال مبهم، مدل clarification کوتاه بخواهد یا فرض خود را بنویسد.
- streaming از backend به browser انجام شود و client مستقیماً API key را دریافت نکند.
- خطای provider، rate limit و timeout به فارسی و بدون افشای body حساس نمایش داده شود.
- تاریخچهٔ گفت‌وگو قابل‌حذف و دارای سقف نگه‌داری باشد.

## ۱۷. امنیت و حریم داده

### کنترل‌های اصلی

- TLS اجباری و bind برنامه فقط روی `127.0.0.1` یا Unix socket.
- استفاده از `http.CrossOriginProtection` نسخهٔ امن Go، Origin check و ممنوعیت CORS عمومی.
- state-changing endpointها فقط POST/PUT/PATCH/DELETE؛ GET بدون side effect.
- CSP سخت، `frame-ancestors` قابل‌تنظیم برای فرم public، `nosniff` و Referrer-Policy.
- output escaping پیش‌فرض templateها؛ HTML کاربر render نشود.
- query پارامتری؛ عدم concatenate ورودی در SQL.
- rate limit حافظه‌ای به تفکیک IP/slug و limit سراسری؛ proxyهای قابل‌اعتماد صریح تنظیم شوند.
- honeypot و minimum-fill-time؛ Turnstile اختیاری به تفکیک پروژه با validation اجباری server-side.
- logها فاقد answer، password، session token، API key و file content باشند.
- audit برای عملیات حساس و download/export داده.
- idempotency key برای submit، publish، backup request و restore request.

### SSRF برای endpoint مدل

- فقط مدیر endpoint را تغییر دهد.
- HTTPS اجباری؛ HTTP فقط برای loopback با گزینهٔ صریح توسعه.
- schemeهای غیر HTTP(S)، userinfo، redirect و hostname مبهم رد شوند.
- resolve DNS و مسدودکردن metadata/link-local/private IP مگر allowlist صریح برای provider داخلی.
- IP پس از redirect دوباره بررسی شود و redirect به‌طور پیش‌فرض خاموش باشد.

### ریسک باقی‌ماندهٔ فایل

برای حفظ سبکی، ClamAV اجباری نیست. جبران ریسک با allowlist سخت، بررسی magic bytes، عدم parse غیرضروری، عدم اجرای فایل، ذخیره خارج web root، download اجباری و limit انجام می‌شود. در محیطی که فایل‌های ناشناس زیاد دریافت می‌شود، افزودن scanner خارجی یک hardening اختیاری و مستند باشد.

## ۱۸. کارایی و بودجهٔ منابع

این موارد acceptance budget هستند و باید اندازه‌گیری شوند، نه صرفاً ادعا:

- process اصلی در حالت idle هدف کمتر از ۸۰MB RSS؛ سقف نهایی پس از benchmark تعیین شود.
- pool برنامه: آغاز با حداکثر ۶ connection و حداقل صفر/یک؛ بدون PgBouncer.
- JavaScript اولیهٔ فرم public هدف کمتر از ۵۰KB gzip، بدون chart/flow code.
- bundle اولیهٔ admin هدف کمتر از ۱۸۰KB gzip؛ flow و chart هرکدام chunk جدا و lazy.
- LCP مسیر public هدف حداکثر ۲٫۵ ثانیه در صدک ۷۵ روی موبایل متعارف.
- p95 پاسخ endpointهای معمول زیر ۲۰۰ms روی dataset تست، بدون احتساب upload/AI/export.
- submit و upload streaming؛ هیچ body نامحدود یا `ReadAll` روی ورودی کاربر.
- export XLSX و backup با temporary file/stream و حافظهٔ bounded.
- queryهای گزارش روی dataset حداقل ۱۰۰هزار submission benchmark و `EXPLAIN ANALYZE` شوند.
- index فقط برای query واقعی؛ indexهای submission بر project/version/time/deleted و answerهای پرتکرار.
- cache-control immutable برای assetهای hash‌شده و ETag برای schema public.
- compression در reverse proxy؛ پاسخ‌های از پیش فشرده‌شدهٔ asset در build در صورت توجیه benchmark.

برای سرور مشترک، تنظیم global PostgreSQL کورکورانه تغییر نکند. ابتدا با default Debian، connection pool کوچک و query/index درست اندازه‌گیری شود. محدودیت systemd پس از load test تنظیم و در راهنمای نصب قابل‌تغییر نوشته شود.

## ۱۹. عملیات و observability

- log ساختاریافته به stdout/journald با request ID.
- endpointهای `/health/live` و `/health/ready`؛ ready شامل اتصال DB، migration و writable بودن storage است.
- صفحهٔ «سلامت سیستم» فقط برای مدیر: app/schema version، مصرف disk، وضعیت DB، آخرین backup و timerها.
- graceful shutdown، timeoutهای read header/read/write/idle و محدودیت concurrent export/backup/AI.
- subcommandها:
  - `forms serve`
  - `forms migrate up|status`
  - `forms admin create|reset-password`
  - `forms backup create|verify|list|prune`
  - `forms restore verify|apply|pending`
  - `forms cleanup sessions|trash|temp`
  - `forms doctor`
  - `forms version`
- release archive شامل باینری، checksum/signature، service files، proxy samples، licenseها و migration notes.

## ۲۰. تست و کنترل کیفیت

### تست backend

- unit: flow evaluator، schema validation، Persian date، normalization، role policy و import validation.
- integration با PostgreSQL واقعی: migration، repository، publish transaction، submit/idempotency و report query.
- property/fuzz: parser schema، condition tree، zip/tar import، filename و date parsing.
- golden fixture: مسیرهای form با پاسخ ورودی و page/ending/assignment مورد انتظار.
- race detector برای packageهای دارای concurrency.

### تست frontend

- component test برای field editor، rule builder، date picker و error states.
- Playwright برای مسیرهای واقعی کاربر در Chromium/Firefox/WebKit و viewport موبایل.
- سناریوهای E2E: ساخت تا انتشار، همهٔ branchها، back/change answer، upload، submit تکراری، report/export و restore staging.
- screenshot regression فقط برای صفحه‌های کلیدی با environment ثابت.
- axe automated scan همراه با بازبینی دستی keyboard/focus/RTL؛ تست خودکار جای بررسی دستی را نگیرد.

### امنیت و dependency

- `go test ./...`, `go vet`, formatter و lint محدود و ثابت.
- `govulncheck ./...`.
- audit وابستگی npm و lockfile ثابت.
- SBOM و license report در release.
- secret scanning و تست عدم log شدن token/answer.

### دادهٔ تست

- generator deterministic برای پروژهٔ استخدام چندمسیره، survey NPS و فرم ثبت‌نام.
- dataset کوچک برای E2E و dataset ۱۰۰هزار submission برای performance.
- متن فارسی، نیم‌فاصله، ی/ک عربی، emoji، ارقام سه‌گانه و filenameهای RTL.

## ۲۱. برنامهٔ اجرای چهارمرحله‌ای

قاعدهٔ اجرا: هر مرحله در یک اجرای مستقل GPT-5.6 Sol Max انجام شود. عامل پیاده‌ساز ابتدا این سند و وضعیت مخزن را بخواند، فقط scope همان مرحله را انجام دهد، تست و گزارش تحویل بدهد و متوقف شود. شروع مرحلهٔ بعد منوط به تأیید خروجی و معیار خروج همان مرحله است.

### مرحلهٔ ۱ — پایه، طراحی سیستم و مدیریت اولیه

**هدف:** یک skeleton production-grade با login، کاربران، پروژه‌ها، پایهٔ فارسی/شمسی و draft versioned.

کارها:

1. ایجاد ساختار Go/Svelte/Vite، build reproducible و embed assetها.
2. script دریافت Vazirmatn از release رسمی با pin/checksum/license.
3. PostgreSQL connection، migration runner، schema جدول‌های پایه و repositoryها.
4. config از env/file با validation، structured log، health و graceful shutdown.
5. Argon2id، session، سه نقش، CLI ساخت/reset مدیر و audit پایه.
6. shell پنل فارسی RTL، login، dashboard پروژه‌ها، ساخت/ویرایش/زباله‌دان پروژه.
7. componentهای design system، normalization فارسی و Persian date picker مشترک.
8. form draft schema حداقلی، autosave با revision و conflict handling.
9. builder پایه برای page و فیلدهای متن، عدد، choice، تاریخ و بلوک توضیحی؛ هنوز انتشار عمومی کامل لازم نیست.
10. تست unit/integration/component و CI اولیه.

معیار خروج:

- باینری واحد ساخته و بدون Node runtime اجرا شود.
- مدیر با CLI ساخته، login کند، کاربر/پروژه بسازد و نقش‌ها واقعاً enforce شوند.
- draft با conflict test ذخیره شود.
- date picker کاملاً شمسی و round-trip server/browser تست شود.
- bundle budget اولیه و idle memory ثبت شود.
- هیچ قابلیت مرحلهٔ ۲ یا mock بی‌کیفیت به اسم تکمیل‌شده معرفی نشود.

### مرحلهٔ ۲ — فرم‌ساز کامل، منطق و انتشار عمومی

**هدف:** ساخت و انتشار یک فرم چندصفحه‌ای واقعی با شاخه‌ها، فایل و همهٔ fieldهای توافق‌شده.

کارها:

1. تکمیل field catalog، validation و inspectorهای type-specific.
2. reorder دسترس‌پذیر، duplicate، page management، preview desktop/mobile و سه theme.
3. schema validator و publish snapshot immutable.
4. flow domain، evaluator deterministic، شرط‌های typed، assignment و graph validation.
5. Svelte Flow lazy-loaded، rule builder، هشدار graph و test-path mode.
6. صفحات public server-rendered، public session موقت و history/back.
7. upload streaming/staging/finalize/cleanup و signature.
8. rate limit، honeypot، idempotency و Turnstile اختیاری با server verification.
9. تغییر پاسخ قبلی و prune مسیر نامعتبر؛ submit نهایی server-authoritative.
10. E2E کامل سه پروژهٔ نمونه و accessibility مسیر عمومی.

معیار خروج:

- فرم استخدام نمونه با حداقل سه شاخه و دو ending منتشر و پاسخ معتبر ثبت شود.
- cycle/reference/fallback خطا قبل از publish گرفته شود.
- تغییر جواب در back navigation دادهٔ شاخهٔ قبلی را حذف کند.
- upload بزرگ‌تر از limit بدون رشد کنترل‌نشدهٔ RAM رد شود.
- public form بدون JavaScript نیز برای فیلدهای غیرتقویمی قابل‌تکمیل باشد و تاریخ fallback متنی شمسی داشته باشد.
- بودجهٔ public JS/LCP و تست موبایل ثبت و پاس شود.

### مرحلهٔ ۳ — پاسخ، گزارش، انتقال و AI

**هدف:** چرخهٔ بهره‌برداری از داده کامل شود، بدون افزودن infrastructure جدید.

کارها:

1. جدول/جزئیات پاسخ، جست‌وجو، filter، pagination و soft delete.
2. aggregationهای گزارش، analytics روزانه و نمودارهای lazy.
3. گزارش چاپی HTML/SVG با فیلتر و تاریخ شمسی.
4. export CSV/XLSX/JSON با streaming و محافظت formula injection.
5. project package export/import با version/checksum و validation امنیتی.
6. abstraction provider AI با Responses و Chat Completions mode و compatibility test.
7. secret storage، SSRF guard، SSE streaming و project opt-in.
8. ابزارهای query read-only، limitها، aggregate-only default و text-sample consent.
9. Markdown/chart spec امن و تاریخچهٔ قابل‌حذف گفتگو.
10. benchmark query/report/export روی dataset بزرگ و E2E.

معیار خروج:

- export/import پروژه round-trip برابر معنایی بدهد.
- CSV/XLSX/JSON روی دادهٔ فارسی و typed درست باز شوند.
- report چاپی A4 در Chromium/Firefox خوانا باشد و وابستگی browser روی سرور نداشته باشد.
- مدل نتواند SQL دلخواه یا tool خارج project اجرا کند.
- خاموش بودن AI هیچ درخواست خارجی نسازد؛ aggregate-only هیچ متن خامی ارسال نکند.
- provider ناسازگار با پیام تشخیصی روشن شناسایی شود.

### مرحلهٔ ۴ — بکاپ/بازیابی، سخت‌سازی، استقرار و مستندات

**هدف:** release قابل‌اعتماد برای Debian و تحویل عملیاتی کامل.

کارها:

1. backup archive، manifest/checksum، maintenance lock، verify و prune.
2. restore staging، safety backup، pending state، systemd handoff، rollback و audit.
3. systemd service/timerهای hardened و محدودیت منابع پس از benchmark.
4. Caddy config پیش‌فرض و Nginx config جایگزین؛ trusted proxy و TLS headers.
5. packaging release، checksum/signature، SBOM و migration/upgrade policy.
6. security review طبق OWASP، upload/CSRF/session/SSRF tests و dependency scans.
7. load/performance/accessibility/RTL cross-browser pass و رفع regression.
8. نگارش و **آزمایش واقعی** مستندات فارسی روی Debian تمیز.
9. doctor command، troubleshooting و runbook خرابی DB/disk/restore.
10. release candidate، restore drill و ثبت معیارهای نهایی.

معیار خروج:

- نصب از صفر فقط با دنبال‌کردن مستندات روی یک VM تمیز موفق شود.
- restart/reboot سرویس و timerها را سالم بالا بیاورد.
- backup گرفته، روی محیط جدا restore و با checksum/sample queries تأیید شود.
- upgrade از release قبلی و rollback مستند آزمایش شود.
- processها از budget توافقی عبور نکنند و سرویس‌های دیگر سرور مختل نشوند.
- release هیچ secret، دادهٔ نمونهٔ حساس یا dependency runtime اعلام‌نشده نداشته باشد.

## ۲۲. مستندات الزامی که باید همراه برنامه ساخته شوند

این فایل‌ها خروجی خود پیاده‌سازی‌اند و باید با commandهای واقعی همان release نوشته و تست شوند:

- `README_FA.md`: معرفی، قابلیت‌ها، screenshot، نیازمندی‌ها و quick start.
- `docs/INSTALL-FA.md`: نصب از صفر Debian، PostgreSQL، کاربر سیستم، directoryها، config، migration، ساخت مدیر، Caddy/Nginx، systemd و firewall.
- `docs/CONFIGURATION-FA.md`: تمام env/configها، default، مثال و اثر امنیتی.
- `docs/ADMIN-GUIDE-FA.md`: کاربران، پروژه، فرم، مسیر، انتشار، پاسخ، گزارش، AI و trash.
- `docs/BACKUP-RESTORE-FA.md`: دستی، timer/cron، retention، verify، restore drill و disaster recovery.
- `docs/UPGRADE-FA.md`: backup، تعویض artifact، migration، health check و rollback.
- `docs/TROUBLESHOOTING-FA.md`: log، doctor، permission، DB، proxy، upload، AI و کمبود disk.
- `docs/DEVELOPMENT.md`: toolchain build، test، fixtures، architecture boundaries و release.
- `docs/SECURITY.md`: threat model خلاصه، گزارش آسیب‌پذیری، secret policy و ریسک فایل.

`INSTALL-FA.md` باید حداقل دو مسیر داشته باشد:

1. سرور تازه با PostgreSQL و Caddy از صفر.
2. سروری که PostgreSQL یا Nginx/Caddy موجود دارد، بدون overwrite کردن تنظیمات فعلی.

راهنما باید روی Debian stable فعلی و stable قبلی در CI/VM بررسی شود. script نصب می‌تواند کمک‌کننده باشد، اما نباید جای مستندات قابل‌فهم و commandهای دستی را بگیرد.

## ۲۳. Definition of Done کل پروژه

پروژه زمانی کامل است که:

- تمام scope این سند یا پیاده‌سازی یا صریحاً با دلیل و تأیید از scope خارج شده باشد.
- هر چهار مرحله معیار خروج خود را پاس کرده باشند.
- form versioning و restore با دادهٔ واقعی آزمایش شده باشند.
- همهٔ عملیات تاریخ در UI شمسی باشند و تست regression داشته باشند.
- سه نقش و تمام endpointها authorization test داشته باشند.
- فایل private، stream و محدود باشد.
- public form، builder، flow، reports و printable view در RTL و موبایل QA شوند.
- راهنمای نصب روی Debian تمیز بدون دانش ضمنی اجرا شده باشد.
- dependency/runtime list، license، SBOM، checksum و release note کامل باشند.
- هیچ TODO بحرانی، migration آزمایش‌نشده یا backup تأییدنشده باقی نمانده باشد.

## ۲۴. ریسک‌ها و تصمیم‌های احتیاطی

| ریسک | کنترل پیشنهادی |
|---|---|
| بزرگ‌شدن scope فیلدها | registry type و تحویل fieldها در دسته‌ها؛ معیار مشترک validation/report/export |
| اختلاف evaluator و UI | server-authoritative evaluator و fixtureهای golden |
| گم‌شدن معنای پاسخ پس از تغییر فرم | snapshot immutable و UUID پایدار |
| مصرف RAM در upload/export | streaming، hard limit و concurrency semaphore |
| پیچیدگی restore از پنل | state machine خارج process وب و safety backup |
| APIهای «سازگار» ولی ناقص | دو protocol mode و compatibility test |
| prompt injection از پاسخ‌ها | داده به‌عنوان untrusted، ابزارهای read-only و بدون SQL آزاد |
| خطای شمسی/UTC | storage استاندارد، boundary conversion و fixture مشترک دو زبان |
| شلوغ‌شدن UI | progressive disclosure، tabهای روشن و advanced optionهای جمع‌شده |
| وابستگی frontend رو به رشد | بودجه bundle، lockfile، lazy chunk و ممنوعیت UI kit بزرگ |
| پرشدن disk با فایل | quota، free-space threshold، cleanup timer و نمایش سلامت |
| نبود اسکن بدافزار | allowlist سخت، private storage، attachment download و ClamAV اختیاری |

## ۲۵. منابع اصلی تحقیق و مستندات

### الگوهای محصول

- [Tally conditional logic](https://tally.so/help/conditional-form-logic)
- [Typeform Logic Map](https://help.typeform.com/hc/en-us/articles/360057591531-Logic-Map)
- [Typeform interactive Logic Map](https://help.typeform.com/hc/en-us/articles/5514792640916-Use-the-Logic-Map-to-add-Logic-to-your-forms)
- [Jotform page skipping and condition priority](https://www.jotform.com/help/350-how-to-skip-pages-using-conditional-logic/)
- [Google Forms section branching](https://support.google.com/docs/answer/141062?hl=en)

### فناوری و عملیات

- [Go embed package](https://go.dev/pkg/embed/)
- [Go net/http and CrossOriginProtection](https://pkg.go.dev/net/http)
- [pgx PostgreSQL driver](https://github.com/jackc/pgx)
- [PostgreSQL data types and JSONB](https://www.postgresql.org/docs/current/datatype.html)
- [PostgreSQL pg_dump](https://www.postgresql.org/docs/current/app-pgdump.html)
- [Svelte](https://svelte.dev/)
- [Svelte Flow](https://svelteflow.dev/)
- [Chart.js performance](https://www.chartjs.org/docs/latest/general/performance.html)
- [Excelize streaming writer](https://github.com/qax-os/excelize/blob/master/stream.go)
- [Vazirmatn releases](https://github.com/rastikerdar/vazirmatn/releases)
- [jalaali-js](https://github.com/jalaali/jalaali-js)
- [go-jalaali](https://github.com/jalaali/go-jalaali)
- [Caddy automatic HTTPS](https://caddyserver.com/docs/automatic-https)
- [PostgreSQL on Debian](https://www.postgresql.org/download/linux/debian/)

### امنیت، دسترس‌پذیری و AI

- [OWASP File Upload Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/File_Upload_Cheat_Sheet.html)
- [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
- [OWASP CSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html)
- [OWASP Password Storage Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)
- [Cloudflare Turnstile server validation](https://developers.cloudflare.com/turnstile/get-started/server-side-validation/)
- [W3C WCAG 2.2 error identification](https://www.w3.org/WAI/WCAG22/Understanding/error-identification)
- [Playwright accessibility testing](https://playwright.dev/docs/accessibility-testing)
- [OpenAI Responses API](https://developers.openai.com/api/reference/cli/resources/responses/methods/create)
- [OpenAI Chat Completions API](https://developers.openai.com/api/reference/cli/resources/chat/subresources/completions)

در آغاز هر مرحله، نسخه و وضعیت نگهداری dependencyها دوباره بررسی و نسخهٔ دقیق در lockfile pin شود. این سند نام major یا نسخهٔ patch آینده را hard-code نمی‌کند تا پیاده‌ساز به نسخهٔ منسوخ متعهد نشود؛ release باید با آخرین نسخهٔ stable و security-patched سازگارِ زمان اجرای همان مرحله ساخته شود.
