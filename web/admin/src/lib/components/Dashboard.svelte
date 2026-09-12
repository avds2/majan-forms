<script lang="ts">
  import { onMount } from 'svelte';
  import { api, APIError } from '../api';
  import { formatDateTime, normalizeDigits, toPersianDigits } from '../persian-date';
  import type { Project, User } from '../types';
  import Modal from './Modal.svelte';

  export let user: User;
  export let onNavigate: (path: string) => void;

  let projects: Project[] = [];
  let loading = true;
  let error = '';
  let createOpen = false;
  let trashTarget: Project | null = null;
  let title = '';
  let slug = '';
  let slugTouched = false;
  let submitting = false;

  $: canEdit = user.role === 'admin' || user.role === 'designer';

  onMount(load);

  async function load() {
    loading = true;
    error = '';
    try {
      projects = await api.projects();
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      loading = false;
    }
  }

  function titleChanged(value: string) {
    title = value;
    if (!slugTouched) slug = slugify(value);
  }

  function slugify(value: string): string {
    const normalized = normalizeDigits(value)
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-|-$/g, '')
      .slice(0, 63);
    return normalized || `form-${Date.now().toString(36)}`;
  }

  function openCreate() {
    title = '';
    slug = '';
    slugTouched = false;
    error = '';
    createOpen = true;
  }

  async function createProject() {
    submitting = true;
    error = '';
    try {
      const created = await api.createProject(title, slug);
      createOpen = false;
      onNavigate(`/projects/${created.id}/builder`);
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      submitting = false;
    }
  }

  async function trashProject() {
    if (!trashTarget) return;
    submitting = true;
    try {
      await api.trashProject(trashTarget.id);
      projects = projects.filter((project) => project.id !== trashTarget?.id);
      trashTarget = null;
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      submitting = false;
    }
  }

  function messageOf(caught: unknown): string {
    return caught instanceof APIError ? caught.message : 'ارتباط با سرور برقرار نشد.';
  }

  function statusLabel(status: Project['status']): string {
    return { draft: 'پیش‌نویس', published: 'منتشرشده', closed: 'متوقف', trash: 'زباله‌دان' }[status];
  }
</script>

<div class="page-container dashboard-page">
  <header class="page-header">
    <div>
      <p class="eyebrow">فضای کاری</p>
      <h1>پروژه‌ها</h1>
      <p>هر پروژه، فرم و پیش‌نویس مستقل خودش را دارد.</p>
    </div>
    {#if canEdit}
      <button class="button primary" type="button" onclick={openCreate}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
        پروژهٔ تازه
      </button>
    {/if}
  </header>

  <section class="summary-strip" aria-label="خلاصهٔ پروژه‌ها">
    <div><span>کل پروژه‌ها</span><strong>{toPersianDigits(projects.length)}</strong></div>
    <div>
      <span>منتشرشده</span><strong
        >{toPersianDigits(projects.filter((project) => project.status === 'published').length)}</strong
      >
    </div>
    <div class="phase-note">
      <span class="status-dot"></span>
      <p><strong>ساخت و انتشار فعال است</strong><small>مسیرهای شرطی و فرم عمومی آمادهٔ استفاده‌اند.</small></p>
    </div>
  </section>

  {#if error && !createOpen}<div class="alert error" role="alert">
      {error}<button type="button" onclick={load}>تلاش دوباره</button>
    </div>{/if}

  {#if loading}
    <div class="project-grid" aria-label="در حال بارگذاری">
      {#each [1, 2, 3] as _}<div class="project-card skeleton-card"><i></i><i></i><i></i></div>{/each}
    </div>
  {:else if projects.length === 0}
    <section class="empty-state">
      <div class="empty-graphic" aria-hidden="true">
        <span></span><span></span><span></span>
        <svg viewBox="0 0 24 24"><path d="M12 5v14M5 12h14" /></svg>
      </div>
      <h2>هنوز پروژه‌ای ساخته نشده</h2>
      <p>{canEdit ? 'اولین فرم سازمان را با یک پروژهٔ تازه شروع کنید.' : 'هنوز پروژه‌ای برای مشاهده وجود ندارد.'}</p>
      {#if canEdit}<button class="button primary" type="button" onclick={openCreate}>ساخت نخستین پروژه</button>{/if}
    </section>
  {:else}
    <div class="section-heading">
      <h2>همهٔ پروژه‌ها</h2>
      <span>{toPersianDigits(projects.length)} مورد</span>
    </div>
    <div class="project-grid">
      {#each projects as project}
        <article class="project-card">
          <button
            class="project-open"
            type="button"
            onclick={() => onNavigate(`/projects/${project.id}/builder`)}
            aria-label={`باز کردن ${project.title}`}
          >
            <div class="project-icon" aria-hidden="true">
              <svg viewBox="0 0 24 24"><path d="M8 7h8M8 11h8M8 15h5M6 3h12a2 2 0 0 1 2 2v14H4V5a2 2 0 0 1 2-2Z" /></svg
              >
            </div>
            <div class="project-copy">
              <div class="project-title-line">
                <h3>{project.title}</h3>
                <span class={`badge ${project.status}`}>{statusLabel(project.status)}</span>
              </div>
              <p class="ltr-slug">/f/{project.slug}</p>
              <p class="metadata">آخرین تغییر: {formatDateTime(project.updated_at)}</p>
            </div>
          </button>
          <div class="project-actions">
            <button class="button subtle" type="button" onclick={() => onNavigate(`/projects/${project.id}/builder`)}>
              {canEdit ? 'ویرایش فرم' : 'مشاهدهٔ فرم'}
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 18 6-6-6-6" /></svg>
            </button>
            <button class="button subtle" type="button" onclick={() => onNavigate(`/projects/${project.id}/routes`)}
              >مسیرها</button
            >
            <button class="button subtle" type="button" onclick={() => onNavigate(`/projects/${project.id}/publish`)}
              >انتشار</button
            >
            {#if project.status === 'published' || project.status === 'closed'}<a
                class="button subtle"
                href={`/f/${project.slug}`}
                target="_blank"
                rel="noopener noreferrer">فرم عمومی</a
              >{/if}
            {#if canEdit}
              <button
                class="icon-button ghost danger-hover"
                type="button"
                aria-label={`انتقال ${project.title} به زباله‌دان`}
                onclick={() => (trashTarget = project)}
              >
                <svg viewBox="0 0 24 24" aria-hidden="true"
                  ><path d="M4 7h16m-11 4v6m6-6v6M9 4h6l1 3H8zm-3 3 1 14h10l1-14" /></svg
                >
              </button>
            {/if}
          </div>
        </article>
      {/each}
    </div>
  {/if}
</div>

{#if createOpen}
  <Modal
    title="پروژهٔ تازه"
    description="برای هر پروژه یک فرم و پیش‌نویس مستقل ساخته می‌شود."
    onClose={() => (createOpen = false)}
  >
    <form
      class="modal-form"
      onsubmit={(event) => {
        event.preventDefault();
        createProject();
      }}
    >
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
      <div class="field-group">
        <label for="project-title">عنوان پروژه</label>
        <input
          id="project-title"
          value={title}
          oninput={(event) => titleChanged(event.currentTarget.value)}
          placeholder="مثلاً فرم استخدام پاییز"
          required
        />
      </div>
      <div class="field-group">
        <label for="project-slug">نشانی کوتاه</label>
        <div class="input-affix ltr">
          <span>/f/</span><input
            id="project-slug"
            dir="ltr"
            bind:value={slug}
            oninput={() => (slugTouched = true)}
            pattern={'[a-z0-9][a-z0-9-]{1,61}[a-z0-9]'}
            required
          />
        </div>
        <small>فقط حروف انگلیسی کوچک، عدد و خط تیره؛ ۳ تا ۶۳ نویسه.</small>
      </div>
      <div class="modal-actions">
        <button class="button secondary" type="button" onclick={() => (createOpen = false)}>انصراف</button><button
          class="button primary"
          type="submit"
          disabled={submitting}>{submitting ? 'در حال ساخت…' : 'ساخت پروژه'}</button
        >
      </div>
    </form>
  </Modal>
{/if}

{#if trashTarget}
  <Modal
    title="انتقال به زباله‌دان"
    description={`پروژهٔ «${trashTarget.title}» از فهرست فعال پنهان می‌شود و مدیر می‌تواند آن را بازیابی کند.`}
    onClose={() => (trashTarget = null)}
  >
    <div class="modal-actions">
      <button class="button secondary" type="button" onclick={() => (trashTarget = null)}>انصراف</button><button
        class="button danger"
        type="button"
        disabled={submitting}
        onclick={trashProject}>{submitting ? 'در حال انتقال…' : 'انتقال به زباله‌دان'}</button
      >
    </div>
  </Modal>
{/if}
