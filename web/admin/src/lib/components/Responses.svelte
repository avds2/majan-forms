<script lang="ts">
  import { onMount } from 'svelte';
  import { api, APIError } from '../api';
  import { toPersianDigits } from '../persian-date';
  import type { FormVersion, Project, ResponseDetail, ResponseList, ResponseQuery, User } from '../types';
  import DatePicker from './DatePicker.svelte';
  import Modal from './Modal.svelte';
  import ProjectTabs from './ProjectTabs.svelte';

  export let user: User;
  export let projectID: string;
  export let onNavigate: (path: string) => void;

  let project: Project | null = null;
  let versions: FormVersion[] = [];
  let result: ResponseList = { items: [], pagination: { page: 1, page_size: 25, total: 0, total_pages: 0 } };
  let loading = true;
  let tableLoading = false;
  let busy = false;
  let error = '';
  let success = '';
  let search = '';
  let version = '';
  let ending = '';
  let from = '';
  let to = '';
  let deleted: 'active' | 'deleted' | 'all' = 'active';
  let sort: 'newest' | 'oldest' | 'duration_desc' | 'duration_asc' = 'newest';
  let page = 1;
  let selected = new Set<string>();
  let detail: ResponseDetail | null = null;
  let detailLoading = false;

  $: canEdit = user.role === 'admin' || user.role === 'designer';
  $: allSelected = result.items.length > 0 && result.items.every((item) => selected.has(item.id));

  onMount(load);

  async function load() {
    loading = true;
    error = '';
    try {
      [project, versions] = await Promise.all([api.project(projectID), api.versions(projectID)]);
      await loadResponses();
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      loading = false;
    }
  }

  function currentQuery(): ResponseQuery {
    return {
      page,
      page_size: 25,
      search: search.trim() || undefined,
      version: version ? Number(version) : undefined,
      ending: ending || undefined,
      from: from || undefined,
      to: to || undefined,
      deleted,
      sort
    };
  }

  async function loadResponses() {
    tableLoading = true;
    error = '';
    try {
      result = await api.responses(projectID, currentQuery());
      selected = new Set();
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      tableLoading = false;
    }
  }

  async function applyFilters() {
    page = 1;
    await loadResponses();
  }

  async function movePage(target: number) {
    if (target < 1 || target > result.pagination.total_pages || target === page) return;
    page = target;
    await loadResponses();
    window.scrollTo({ top: 0, behavior: 'smooth' });
  }

  function toggleSelection(id: string, checked: boolean) {
    const next = new Set(selected);
    if (checked) next.add(id);
    else next.delete(id);
    selected = next;
  }

  function togglePage(checked: boolean) {
    const next = new Set(selected);
    for (const item of result.items) {
      if (checked) next.add(item.id);
      else next.delete(item.id);
    }
    selected = next;
  }

  async function openDetail(id: string) {
    detailLoading = true;
    error = '';
    try {
      detail = await api.response(projectID, id);
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      detailLoading = false;
    }
  }

  async function changeDeletedState() {
    if (!selected.size || !canEdit) return;
    const restoring = deleted === 'deleted';
    const verb = restoring ? 'بازگردانی' : 'حذف';
    if (!window.confirm(`${verb} ${toPersianDigits(selected.size)} پاسخ انجام شود؟`)) return;
    busy = true;
    error = '';
    success = '';
    try {
      const ids = [...selected];
      const changed = restoring
        ? await api.restoreResponses(projectID, ids)
        : await api.deleteResponses(projectID, ids);
      success = `${toPersianDigits(changed.updated)} پاسخ ${restoring ? 'بازگردانده' : 'حذف نرم'} شد.`;
      await loadResponses();
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      busy = false;
    }
  }

  function endingOptions(): Array<{ id: string; label: string; version: number }> {
    const seen = new Set<string>();
    const options: Array<{ id: string; label: string; version: number }> = [];
    for (const item of versions) {
      if (version && item.version_number !== Number(version)) continue;
      for (const value of item.schema.endings) {
        if (seen.has(value.id)) continue;
        seen.add(value.id);
        options.push({ id: value.id, label: value.title, version: item.version_number });
      }
    }
    return options;
  }

  function exportURL(format: 'csv' | 'xlsx' | 'json'): string {
    const query = currentQuery();
    delete query.page;
    delete query.page_size;
    return api.exportURL(projectID, format, query);
  }

  function duration(seconds: number): string {
    if (seconds < 60) return `${toPersianDigits(seconds)} ثانیه`;
    const minutes = Math.floor(seconds / 60);
    const remainder = seconds % 60;
    return `${toPersianDigits(minutes)} دقیقه${remainder ? ` و ${toPersianDigits(remainder)} ثانیه` : ''}`;
  }

  function fileSize(bytes: number): string {
    if (bytes < 1024) return `${toPersianDigits(bytes)} بایت`;
    if (bytes < 1024 * 1024) return `${toPersianDigits((bytes / 1024).toFixed(1))} کیلوبایت`;
    return `${toPersianDigits((bytes / (1024 * 1024)).toFixed(1))} مگابایت`;
  }

  function messageOf(caught: unknown): string {
    return caught instanceof APIError ? caught.message : 'ارتباط با سرور برقرار نشد.';
  }
</script>

{#if loading}
  <div class="builder-loading">
    <div class="loader"></div>
    <span>در حال آماده‌سازی پاسخ‌ها…</span>
  </div>
{:else if !project}
  <main class="startup-error embedded">
    <div class="status-orb error">!</div>
    <h1>پاسخ‌ها باز نشد</h1>
    <p>{error}</p>
    <button class="button primary" type="button" onclick={load}>تلاش دوباره</button>
  </main>
{:else}
  <div class="builder-page data-page responses-page">
    <header class="builder-header">
      <div class="builder-title">
        <button class="icon-button ghost" type="button" aria-label="بازگشت به پروژه‌ها" onclick={() => onNavigate('/')}
          ><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 18 6-6-6-6" /></svg></button
        >
        <div>
          <span>پروژه / پاسخ‌ها</span>
          <h1>{project.title}</h1>
        </div>
      </div>
      <div class="save-cluster">
        <span class="response-total">{toPersianDigits(result.pagination.total)} پاسخ</span>
      </div>
    </header>
    <ProjectTabs {projectID} active="responses" {onNavigate} />

    <main class="data-content">
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
      {#if success}<div class="alert success" role="status">{success}</div>{/if}
      <form
        class="response-filters"
        onsubmit={(event) => {
          event.preventDefault();
          applyFilters();
        }}
      >
        <div class="field-group filter-search">
          <label for="response-search">جست‌وجو در پاسخ‌ها</label>
          <div class="search-input">
            <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="m20 20-4-4" /></svg
            ><input id="response-search" bind:value={search} placeholder="نام، ایمیل، تلفن یا شناسهٔ پاسخ" />
          </div>
        </div>
        <div class="field-group">
          <label for="response-version">نسخه</label><select
            id="response-version"
            bind:value={version}
            onchange={() => (ending = '')}
            ><option value="">همهٔ نسخه‌ها</option>{#each versions as item}<option value={item.version_number}
                >نسخهٔ {toPersianDigits(item.version_number)}</option
              >{/each}</select
          >
        </div>
        <div class="field-group">
          <label for="response-ending">پایان</label><select id="response-ending" bind:value={ending}
            ><option value="">همهٔ پایان‌ها</option>{#each endingOptions() as item}<option value={item.id}
                >{item.label}{version ? '' : ` · نسخهٔ ${toPersianDigits(item.version)}`}</option
              >{/each}</select
          >
        </div>
        <DatePicker id="response-from" value={from} label="از تاریخ" onValueChange={(value) => (from = value)} />
        <DatePicker id="response-to" value={to} label="تا تاریخ" onValueChange={(value) => (to = value)} />
        <div class="field-group">
          <label for="response-state">وضعیت</label><select id="response-state" bind:value={deleted}
            ><option value="active">فعال</option><option value="deleted">حذف‌شده</option><option value="all">همه</option
            ></select
          >
        </div>
        <div class="field-group">
          <label for="response-sort">ترتیب</label><select id="response-sort" bind:value={sort}
            ><option value="newest">جدیدترین</option><option value="oldest">قدیمی‌ترین</option><option
              value="duration_desc">طولانی‌ترین تکمیل</option
            ><option value="duration_asc">کوتاه‌ترین تکمیل</option></select
          >
        </div>
        <button class="button primary filter-submit" type="submit" disabled={tableLoading}>اعمال فیلتر</button>
      </form>

      <div class="data-actions">
        <div class="export-actions" aria-label="دریافت خروجی">
          <span>خروجی:</span><a class="button subtle" href={exportURL('csv')}>CSV</a><a
            class="button subtle"
            href={exportURL('xlsx')}>XLSX</a
          ><a class="button subtle" href={exportURL('json')}>JSON</a>
        </div>
        {#if canEdit && selected.size}<button
            class:danger-outline={!deleted || deleted !== 'deleted'}
            class="button secondary"
            type="button"
            disabled={busy}
            onclick={changeDeletedState}>{deleted === 'deleted' ? 'بازگردانی انتخاب‌ها' : 'حذف انتخاب‌ها'}</button
          >{/if}
      </div>

      <section class="table-card responses-table" aria-busy={tableLoading}>
        {#if tableLoading}<div class="table-loading">
            <div class="loader"></div>
            <span>در حال خواندن پاسخ‌ها…</span>
          </div>
        {:else if !result.items.length}<div class="empty-state compact">
            <div class="empty-icon">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 4h14v16H5zM8 8h8M8 12h8M8 16h5" /></svg>
            </div>
            <h2>{deleted === 'deleted' ? 'پاسخ حذف‌شده‌ای نیست' : 'هنوز پاسخی ثبت نشده است'}</h2>
            <p>با ثبت نخستین پاسخ، اطلاعات آن در این بخش دیده می‌شود.</p>
          </div>
        {:else}<div class="responsive-table">
            <table>
              <thead
                ><tr
                  >{#if canEdit}<th class="select-cell"
                      ><input
                        type="checkbox"
                        aria-label="انتخاب همهٔ پاسخ‌های این صفحه"
                        checked={allSelected}
                        onchange={(event) => togglePage((event.currentTarget as HTMLInputElement).checked)}
                      /></th
                    >{/if}<th>شناسه</th><th>زمان ثبت</th><th>نسخه</th><th>پایان</th><th>مدت</th><th></th></tr
                ></thead
              ><tbody
                >{#each result.items as item}<tr class:deleted-row={Boolean(item.deleted_at)}
                    >{#if canEdit}<td class="select-cell" data-label="انتخاب"
                        ><input
                          type="checkbox"
                          aria-label={`انتخاب پاسخ ${item.public_id}`}
                          checked={selected.has(item.id)}
                          onchange={(event) =>
                            toggleSelection(item.id, (event.currentTarget as HTMLInputElement).checked)}
                        /></td
                      >{/if}<td data-label="شناسه"
                      ><code>{item.public_id.slice(0, 8)}</code>{#if item.deleted_at}<span class="deleted-chip"
                          >حذف‌شده</span
                        >{/if}</td
                    ><td data-label="زمان ثبت" class="date-value">{item.submitted_at_jalali}</td><td data-label="نسخه"
                      >نسخهٔ {toPersianDigits(item.version_number)}</td
                    ><td data-label="پایان">{item.ending_title}</td><td data-label="مدت"
                      >{duration(item.duration_seconds)}</td
                    ><td data-label="جزئیات"
                      ><button
                        class="text-button"
                        type="button"
                        disabled={detailLoading}
                        onclick={() => openDetail(item.id)}>مشاهده</button
                      ></td
                    ></tr
                  >{/each}</tbody
              >
            </table>
          </div>{/if}
      </section>
      {#if result.pagination.total_pages > 1}<nav class="pagination" aria-label="صفحه‌بندی پاسخ‌ها">
          <button
            class="button subtle"
            type="button"
            disabled={page <= 1 || tableLoading}
            onclick={() => movePage(page - 1)}>صفحهٔ قبل</button
          ><span>صفحهٔ {toPersianDigits(page)} از {toPersianDigits(result.pagination.total_pages)}</span><button
            class="button subtle"
            type="button"
            disabled={page >= result.pagination.total_pages || tableLoading}
            onclick={() => movePage(page + 1)}>صفحهٔ بعد</button
          >
        </nav>{/if}
    </main>
  </div>
{/if}

{#if detail}
  <Modal
    title={`پاسخ ${detail.public_id.slice(0, 8)}`}
    description={`${detail.submitted_at_jalali} · نسخهٔ ${toPersianDigits(detail.version_number)} · ${detail.ending_title}`}
    onClose={() => (detail = null)}
  >
    <div class="response-detail-meta">
      <span>مدت تکمیل: <strong>{duration(detail.duration_seconds)}</strong></span>{#if detail.deleted_at_jalali}<span
          >حذف‌شده در: <strong>{detail.deleted_at_jalali}</strong></span
        >{/if}
    </div>
    <div class="answer-list">
      {#each detail.answers as answer}<article>
          <div>
            <small>{answer.page_title}</small>
            <h3>{answer.label}</h3>
          </div>
          <p>{answer.display_value}</p>
          {#if answer.files?.length}<div class="answer-files">
              {#each answer.files as file}<a href={file.download_path}
                  ><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3v12m0 0 4-4m-4 4-4-4M5 19h14" /></svg><span
                    >{file.name}<small>{fileSize(file.size_bytes)}</small></span
                  ></a
                >{/each}
            </div>{/if}
        </article>{/each}
    </div>
  </Modal>
{/if}
