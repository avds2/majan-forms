<script lang="ts">
  import { onMount } from 'svelte';
  import { api, APIError } from '../api';
  import { formatDateTime, toPersianDigits } from '../persian-date';
  import type { Draft, FormVersion, Project, User } from '../types';
  import ProjectTabs from './ProjectTabs.svelte';

  export let user: User;
  export let projectID: string;
  export let onNavigate: (path: string) => void;

  let project: Project | null = null;
  let draft: Draft | null = null;
  let versions: FormVersion[] = [];
  let loading = true;
  let busy = false;
  let dirty = false;
  let error = '';
  let success = '';
  let details: Array<{ path: string; message: string }> = [];
  let publishKey = crypto.randomUUID();

  $: canEdit = user.role === 'admin' || user.role === 'designer';
  $: publicURL = project ? `${window.location.origin}/f/${project.slug}` : '';

  onMount(load);

  async function load() {
    loading = true;
    error = '';
    try {
      const [loadedProject, loadedDraft, loadedVersions] = await Promise.all([
        api.project(projectID),
        api.draft(projectID),
        api.versions(projectID)
      ]);
      project = loadedProject;
      draft = loadedDraft;
      versions = loadedVersions;
      normalizeDraft();
      dirty = false;
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      loading = false;
    }
  }

  function normalizeDraft() {
    if (!draft) return;
    draft.schema.theme ||= { preset: 'minimal', alignment: 'start' };
    draft.schema.public_settings ||= { minimum_fill_seconds: 2 };
  }

  function touch() {
    if (!canEdit) return;
    dirty = true;
    error = '';
    success = '';
    details = [];
  }

  async function saveSettings(): Promise<boolean> {
    if (!draft || !canEdit) return false;
    busy = true;
    clearMessages();
    try {
      draft = await api.saveDraft(projectID, draft.revision, structuredClone(draft.schema));
      normalizeDraft();
      dirty = false;
      success = 'تنظیمات ظاهر و انتشار ذخیره شد.';
      return true;
    } catch (caught) {
      captureError(caught);
      return false;
    } finally {
      busy = false;
    }
  }

  async function validateDraft() {
    if (dirty && !(await saveSettings())) return;
    busy = true;
    clearMessages();
    try {
      const result = await api.validatePublish(projectID);
      success = `نسخهٔ پیش‌نویس ${toPersianDigits(result.revision)} برای انتشار معتبر است.`;
    } catch (caught) {
      captureError(caught);
    } finally {
      busy = false;
    }
  }

  async function publishDraft() {
    if (!draft || !canEdit) return;
    if (dirty && !(await saveSettings())) return;
    busy = true;
    clearMessages();
    try {
      await api.validatePublish(projectID);
      const version = await api.publish(projectID, draft.revision, publishKey);
      publishKey = crypto.randomUUID();
      [project, versions] = await Promise.all([api.project(projectID), api.versions(projectID)]);
      success =
        project?.status === 'closed'
          ? `نسخهٔ ${toPersianDigits(version.version_number)} منتشر شد؛ پذیرش پاسخ همچنان متوقف است.`
          : `نسخهٔ ${toPersianDigits(version.version_number)} منتشر شد و اکنون پاسخ می‌پذیرد.`;
    } catch (caught) {
      captureError(caught);
    } finally {
      busy = false;
    }
  }

  async function togglePublicState() {
    if (!project || !canEdit) return;
    busy = true;
    clearMessages();
    try {
      project = project.status === 'closed' ? await api.reopenProject(project.id) : await api.closeProject(project.id);
      success = project.status === 'closed' ? 'پذیرش پاسخ متوقف شد.' : 'پذیرش پاسخ دوباره فعال شد.';
    } catch (caught) {
      captureError(caught);
    } finally {
      busy = false;
    }
  }

  async function copyPublicURL() {
    try {
      await navigator.clipboard.writeText(publicURL);
      success = 'پیوند عمومی کپی شد.';
    } catch {
      error = 'کپی خودکار ممکن نبود؛ پیوند را دستی کپی کنید.';
    }
  }

  function clearMessages() {
    error = '';
    success = '';
    details = [];
  }

  function captureError(caught: unknown) {
    error = messageOf(caught);
    if (caught instanceof APIError) details = caught.body.error?.details ?? [];
  }

  function messageOf(caught: unknown): string {
    return caught instanceof APIError ? caught.message : 'ارتباط با سرور برقرار نشد.';
  }
</script>

{#if loading}
  <div class="builder-loading">
    <div class="loader"></div>
    <span>در حال آماده‌سازی انتشار…</span>
  </div>
{:else if !project || !draft}
  <main class="startup-error embedded">
    <div class="status-orb error">!</div>
    <h1>انتشار باز نشد</h1>
    <p>{error}</p>
    <button class="button primary" type="button" onclick={load}>تلاش دوباره</button>
  </main>
{:else}
  <div class="builder-page publish-page">
    <header class="builder-header">
      <div class="builder-title">
        <button class="icon-button ghost" type="button" aria-label="بازگشت به پروژه‌ها" onclick={() => onNavigate('/')}
          ><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 18 6-6-6-6" /></svg></button
        >
        <div>
          <span>پروژه / انتشار</span>
          <h1>{project.title}</h1>
        </div>
      </div>
      <div class="save-cluster">
        <span class="status-pill {project.status}"
          >{project.status === 'published' ? 'منتشرشده' : project.status === 'closed' ? 'متوقف' : 'پیش‌نویس'}</span
        >
      </div>
    </header>
    <ProjectTabs {projectID} active="publish" {onNavigate} />

    {#if error}<div class="builder-alert alert error" role="alert">
        <span>{error}</span>{#if details.length}<ul>
            {#each details.slice(0, 8) as detail}<li><strong>{detail.path}</strong> — {detail.message}</li>{/each}
          </ul>{/if}
      </div>{/if}
    {#if success}<div class="builder-alert alert success" role="status">{success}</div>{/if}

    <div class="publish-grid">
      <section class="builder-panel publish-settings">
        <div class="panel-heading">
          <div>
            <span class="step-number">۱</span>
            <h2>ظاهر فرم عمومی</h2>
          </div>
          {#if dirty}<span class="unsaved-chip">ذخیره‌نشده</span>{/if}
        </div>
        <div class="theme-options" role="radiogroup" aria-label="قالب ظاهری">
          {#each [{ id: 'minimal', label: 'مینیمال', color: '#008d83' }, { id: 'formal', label: 'رسمی', color: '#1f5f99' }, { id: 'warm', label: 'گرم', color: '#bf6b3d' }] as theme}<label
              class:active={draft.schema.theme.preset === theme.id}
              ><input
                type="radio"
                name="theme"
                value={theme.id}
                checked={draft.schema.theme.preset === theme.id}
                disabled={!canEdit}
                onchange={() => {
                  draft!.schema.theme.preset = theme.id as 'minimal' | 'formal' | 'warm';
                  touch();
                }}
              /><span class="theme-preview" style={`--theme-color:${theme.color}`}><i></i><i></i><i></i></span><strong
                >{theme.label}</strong
              ></label
            >{/each}
        </div>
        <div class="settings-row">
          <div class="field-group">
            <label for="primary-color">رنگ اصلی</label><input
              id="primary-color"
              type="color"
              bind:value={draft.schema.theme.primary_color}
              disabled={!canEdit}
              oninput={touch}
            />
          </div>
          <div class="field-group">
            <label for="form-alignment">تراز محتوا</label><select
              id="form-alignment"
              bind:value={draft.schema.theme.alignment}
              disabled={!canEdit}
              onchange={touch}><option value="start">ابتدای سطر</option><option value="center">وسط‌چین</option></select
            >
          </div>
        </div>
        <div class="field-group">
          <label for="minimum-fill">حداقل زمان تکمیل (ثانیه)</label><input
            id="minimum-fill"
            type="number"
            min="0"
            max="3600"
            bind:value={draft.schema.public_settings.minimum_fill_seconds}
            disabled={!canEdit}
            oninput={touch}
          /><small>ارسال سریع‌تر از این زمان به‌عنوان رفتار خودکار رد می‌شود.</small>
        </div>
        <label class="check-row"
          ><input
            type="checkbox"
            bind:checked={draft.schema.public_settings.turnstile_enabled}
            disabled={!canEdit}
            onchange={touch}
          /><span
            ><strong>فعال‌کردن Turnstile</strong><small
              >فقط در صورتی فعال کنید که کلیدهای سمت سرور پیکربندی شده باشند.</small
            ></span
          ></label
        >
        {#if canEdit}<button class="button secondary" type="button" disabled={!dirty || busy} onclick={saveSettings}
            >{busy ? 'در حال ذخیره…' : 'ذخیرهٔ تنظیمات'}</button
          >{/if}
      </section>

      <section class="builder-panel publish-action">
        <div class="panel-heading">
          <div>
            <span class="step-number">۲</span>
            <h2>بررسی و انتشار</h2>
          </div>
        </div>
        <div class="publication-facts">
          <div><span>بازبینی پیش‌نویس</span><strong>{toPersianDigits(draft.revision)}</strong></div>
          <div><span>تعداد صفحه</span><strong>{toPersianDigits(draft.schema.pages.length)}</strong></div>
          <div><span>تعداد مسیر</span><strong>{toPersianDigits(draft.schema.flow.edges.length)}</strong></div>
          <div><span>نسخه‌های منتشرشده</span><strong>{toPersianDigits(versions.length)}</strong></div>
        </div>
        <p>انتشار، یک snapshot تغییرناپذیر از همین بازبینی می‌سازد. ویرایش‌های بعدی فقط روی پیش‌نویس اثر دارند.</p>
        <div class="publish-buttons">
          <button class="button secondary" type="button" disabled={busy} onclick={validateDraft}>فقط اعتبارسنجی</button
          >{#if canEdit}<button class="button primary" type="button" disabled={busy || dirty} onclick={publishDraft}
              >{busy ? 'در حال بررسی…' : versions.length ? 'انتشار نسخهٔ تازه' : 'انتشار نخستین نسخه'}</button
            >{/if}
        </div>
      </section>

      <section class="builder-panel public-link-card">
        <div class="panel-heading">
          <div>
            <span class="step-number">۳</span>
            <h2>فرم عمومی</h2>
          </div>
        </div>
        {#if versions.length}<p>این نشانی همیشه به نسخهٔ فعال اشاره می‌کند.</p>
          <div class="public-url">
            <code dir="ltr">{publicURL}</code><button
              class="icon-button"
              type="button"
              aria-label="کپی پیوند"
              onclick={copyPublicURL}>⧉</button
            >
          </div>
          <div class="publish-buttons">
            <a class="button primary" href={publicURL} target="_blank" rel="noopener noreferrer">بازکردن فرم عمومی</a
            >{#if canEdit}<button
                class="button {project.status === 'closed' ? 'secondary' : 'danger-outline'}"
                type="button"
                disabled={busy}
                onclick={togglePublicState}>{project.status === 'closed' ? 'فعال‌کردن پذیرش' : 'توقف پذیرش'}</button
              >{/if}
          </div>{:else}<div class="empty-compact">
            <strong>هنوز نسخه‌ای منتشر نشده</strong><span>پس از رفع خطاهای اعتبارسنجی، نخستین نسخه را منتشر کنید.</span>
          </div>{/if}
      </section>
    </div>

    <section class="builder-panel version-history">
      <div class="panel-heading">
        <div>
          <span class="step-number">۴</span>
          <h2>تاریخچهٔ نسخه‌های تغییرناپذیر</h2>
        </div>
      </div>
      {#if versions.length}<div class="version-table" role="table" aria-label="تاریخچهٔ نسخه‌های منتشرشده">
          <div class="version-row heading" role="row">
            <span role="columnheader">نسخه</span><span role="columnheader">زمان انتشار</span><span role="columnheader"
              >شناسهٔ صحت</span
            ><span role="columnheader">وضعیت</span>
          </div>
          {#each versions as version}<div class="version-row" role="row">
              <span role="cell"><strong>نسخهٔ {toPersianDigits(version.version_number)}</strong></span><span role="cell"
                >{formatDateTime(version.published_at)}</span
              ><span role="cell"><code dir="ltr">{version.checksum.slice(0, 12)}…</code></span><span role="cell"
                >{project.active_version_id === version.id ? 'فعال' : 'بایگانی'}</span
              >
            </div>{/each}
        </div>{:else}<div class="empty-compact">
          <strong>تاریخچه خالی است</strong><span>هر انتشار موفق یک ردیف مستقل و غیرقابل‌ویرایش می‌سازد.</span>
        </div>{/if}
    </section>
  </div>
{/if}
