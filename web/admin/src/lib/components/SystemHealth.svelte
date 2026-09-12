<script lang="ts">
  import { onMount } from 'svelte';
  import { api, APIError } from '../api';
  import { toPersianDigits } from '../persian-date';
  import type { SystemBackup, SystemBackupList, SystemHealth } from '../types';
  import Modal from './Modal.svelte';

  let health: SystemHealth | null = null;
  let backups: SystemBackupList | null = null;
  let loading = true;
  let creating = false;
  let error = '';
  let notice = '';
  let pollTimer: number | undefined;
  let restoreTarget: SystemBackup | null = null;
  let restoreConfirmation = '';
  let preparingRestore = false;

  onMount(() => {
    load();
    return () => window.clearTimeout(pollTimer);
  });

  async function load(silent = false) {
    if (!silent) loading = true;
    error = '';
    try {
      [health, backups] = await Promise.all([api.systemHealth(), api.systemBackups()]);
      if (backups.job.running) {
        schedulePoll();
      } else if (silent) {
        if (backups.job.error) {
          notice = '';
          error = backups.job.error;
        } else if (backups.job.backup) {
          notice = 'بکاپ تازه با موفقیت ساخته و بررسی شد.';
        }
      }
    } catch (caught) {
      error = caught instanceof APIError ? caught.message : 'ارتباط با سرور برقرار نشد.';
    } finally {
      if (!silent) loading = false;
    }
  }

  function schedulePoll() {
    window.clearTimeout(pollTimer);
    pollTimer = window.setTimeout(() => load(true), 1500);
  }

  async function createBackup() {
    creating = true;
    error = '';
    notice = '';
    try {
      const job = await api.createSystemBackup();
      if (backups) backups = { ...backups, job };
      notice = 'ساخت بکاپ آغاز شد؛ تا پایان می‌توانید در همین صفحه بمانید.';
      schedulePoll();
    } catch (caught) {
      error = caught instanceof APIError ? caught.message : 'شروع ساخت بکاپ ممکن نشد.';
    } finally {
      creating = false;
    }
  }

  function openRestore(backup: SystemBackup) {
    restoreTarget = backup;
    restoreConfirmation = '';
    error = '';
    notice = '';
  }

  function closeRestore() {
    if (!preparingRestore) restoreTarget = null;
  }

  async function prepareRestore() {
    if (!restoreTarget || restoreConfirmation !== 'بازیابی') return;
    preparingRestore = true;
    error = '';
    try {
      const result = await api.prepareSystemRestore(restoreTarget.name, restoreConfirmation);
      restoreTarget = null;
      notice = result.message;
    } catch (caught) {
      error = caught instanceof APIError ? caught.message : 'ثبت درخواست بازیابی ممکن نشد.';
    } finally {
      preparingRestore = false;
    }
  }

  function bytes(value: number): string {
    if (!value) return 'نامشخص';
    const units = ['بایت', 'KiB', 'MiB', 'GiB', 'TiB'];
    let amount = value;
    let unit = 0;
    while (amount >= 1024 && unit < units.length - 1) {
      amount /= 1024;
      unit += 1;
    }
    return `${toPersianDigits(amount.toFixed(unit > 1 ? 1 : 0))} ${units[unit]}`;
  }
</script>

<div class="workspace-page system-health-page">
  <header class="page-heading split">
    <div>
      <span class="eyebrow">عملیات و نگه‌داری</span>
      <h1>سلامت سیستم</h1>
      <p>وضعیت زندهٔ پایگاه‌داده، فضای ذخیره و آخرین بکاپ.</p>
    </div>
    <button class="button secondary" type="button" onclick={() => load()} disabled={loading}>به‌روزرسانی</button>
  </header>

  {#if error}<div class="alert error" role="alert">{error}</div>{/if}
  {#if notice}<div class="alert info" role="status">{notice}</div>{/if}
  {#if loading}<div class="table-loading">
      <div class="loader"></div>
      <span>در حال بررسی سامانه…</span>
    </div>
  {:else if health}
    <section class="system-status-grid" aria-label="وضعیت سرویس">
      <article>
        <span>نسخهٔ برنامه</span><strong dir="ltr">{health.app.version}</strong><small dir="ltr"
          >{health.app.commit}</small
        >
      </article>
      <article><span>پایگاه‌داده</span><strong class="healthy">آماده</strong><small>اتصال برقرار است</small></article>
      <article>
        <span>نسخهٔ schema</span><strong>{toPersianDigits(health.schema.current)}</strong><small
          >{health.schema.pending ? `${toPersianDigits(health.schema.pending)} migration معوق` : 'به‌روز'}</small
        >
      </article>
      <article>
        <span>حالت نگهداری</span><strong class:healthy={!health.maintenance.write_locked}
          >{health.maintenance.write_locked ? 'فعال' : 'غیرفعال'}</strong
        ><small>{toPersianDigits(health.maintenance.active_writes)} نوشتن فعال</small>
      </article>
    </section>

    <section class="system-disk-grid">
      {#each [{ label: 'داده و فایل‌ها', disk: health.data_disk }, { label: 'بکاپ‌ها', disk: health.backup_disk }] as item}
        <article class="system-card">
          <header>
            <div>
              <span>فضای ذخیره</span>
              <h2>{item.label}</h2>
            </div>
            <span class:ok={item.disk.writable} class="system-chip"
              >{item.disk.writable ? 'قابل نوشتن' : 'نیازمند بررسی'}</span
            >
          </header>
          <dl>
            <div>
              <dt>فضای آزاد</dt>
              <dd>{bytes(item.disk.free_bytes)}</dd>
            </div>
            <div>
              <dt>حجم فایل‌سیستم</dt>
              <dd>{bytes(item.disk.total_bytes)}</dd>
            </div>
          </dl>
          <code dir="ltr">{item.disk.path}</code>
        </article>
      {/each}
    </section>

    <section class="system-card latest-backup">
      <header>
        <div>
          <span>بازیابی‌پذیری</span>
          <h2>آخرین بکاپ موفق</h2>
        </div>
      </header>
      {#if health.latest_backup}
        <dl>
          <div>
            <dt>فایل</dt>
            <dd dir="ltr">{health.latest_backup.name}</dd>
          </div>
          <div>
            <dt>زمان</dt>
            <dd>{health.latest_backup.created_at_jalali}</dd>
          </div>
          <div>
            <dt>اندازه</dt>
            <dd>{bytes(health.latest_backup.size)}</dd>
          </div>
        </dl>
        <code dir="ltr" title="SHA-256">{health.latest_backup.sha256}</code>
      {:else}<div class="report-empty">هنوز بکاپ موفقی در audit ثبت نشده است.</div>{/if}
      <p>بازیابی در پنل ثبت می‌شود و پس از توقف کنترل‌شدهٔ سرویس، به‌صورت آفلاین و همراه با بکاپ ایمنی اجرا می‌شود.</p>
    </section>

    <section class="system-card backup-history">
      <header>
        <div>
          <span>آرشیوهای معتبر</span>
          <h2>تاریخچهٔ بکاپ</h2>
        </div>
        <button
          class="button primary"
          type="button"
          onclick={createBackup}
          disabled={creating || backups?.job.running || health.maintenance.write_locked}
          >{backups?.job.running ? 'در حال ساخت…' : creating ? 'در حال شروع…' : 'ساخت بکاپ تازه'}</button
        >
      </header>
      {#if backups?.job.running}
        <div class="backup-progress" role="status">
          <span class="loader"></span>
          <div><strong>بکاپ در حال ساخت و اعتبارسنجی است</strong><small>{backups.job.started_at_jalali}</small></div>
        </div>
      {/if}
      {#if backups && backups.items.length > 0}
        <div class="responsive-table backup-table">
          <table>
            <thead><tr><th>فایل</th><th>زمان</th><th>نسخه</th><th>اندازه</th><th>وضعیت</th><th>عملیات</th></tr></thead>
            <tbody>
              {#each backups.items as backup}
                <tr>
                  <td data-label="فایل"><strong dir="ltr">{backup.name}</strong></td>
                  <td data-label="زمان">{backup.created_at_jalali || 'نامشخص'}</td>
                  <td data-label="نسخه"
                    ><span dir="ltr">{backup.app_version}</span> / schema {toPersianDigits(backup.schema_version)}</td
                  >
                  <td data-label="اندازه">{bytes(backup.size)}</td>
                  <td data-label="وضعیت"
                    ><span class:ok={backup.valid} class="system-chip">{backup.valid ? 'معتبر' : 'خراب'}</span></td
                  >
                  <td data-label="عملیات">
                    {#if backup.valid}<div class="backup-actions">
                        <a class="button secondary backup-download" href={api.systemBackupURL(backup.name)}>دریافت</a>
                        <button
                          class="button danger-outline backup-download"
                          type="button"
                          disabled={health.schema.latest < backup.schema_version || backups.job.running}
                          title={health.schema.latest < backup.schema_version
                            ? 'این بکاپ به نسخهٔ جدیدتری از سامانه نیاز دارد'
                            : 'بازگرداندن همهٔ داده‌ها به این نسخه'}
                          onclick={() => openRestore(backup)}>بازیابی</button
                        >
                      </div>{:else}<span title={backup.error}>غیرقابل استفاده</span>{/if}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else}
        <div class="report-empty">هنوز فایل بکاپی در پوشهٔ امن سامانه وجود ندارد.</div>
      {/if}
    </section>
  {/if}
</div>

{#if restoreTarget}
  <Modal
    title="بازیابی کامل سامانه"
    description="این عملیات همهٔ داده‌ها و فایل‌ها را به زمان بکاپ انتخاب‌شده برمی‌گرداند."
    dismissible={!preparingRestore}
    onClose={closeRestore}
  >
    <form
      class="modal-form restore-confirmation"
      onsubmit={(event) => {
        event.preventDefault();
        prepareRestore();
      }}
    >
      <div class="restore-warning" role="note">
        <strong>پیش از ادامه مطمئن شوید:</strong>
        <ul>
          <li>تغییرات پس از <span>{restoreTarget.created_at_jalali || restoreTarget.name}</span> حذف می‌شوند.</li>
          <li>سامانه برای مدت کوتاهی از دسترس خارج و دوباره راه‌اندازی می‌شود.</li>
          <li>پیش از اعمال تغییر، یک بکاپ ایمنی خودکار ساخته می‌شود.</li>
        </ul>
      </div>
      <label for="restore-confirmation">برای تأیید، واژهٔ «بازیابی» را وارد کنید</label>
      <input
        id="restore-confirmation"
        autocomplete="off"
        spellcheck="false"
        bind:value={restoreConfirmation}
        disabled={preparingRestore}
      />
      <div class="modal-actions">
        <button class="button secondary" type="button" onclick={closeRestore} disabled={preparingRestore}>انصراف</button
        >
        <button class="button danger" type="submit" disabled={preparingRestore || restoreConfirmation !== 'بازیابی'}
          >{preparingRestore ? 'در حال بررسی…' : 'تأیید و راه‌اندازی مجدد'}</button
        >
      </div>
    </form>
  </Modal>
{/if}
