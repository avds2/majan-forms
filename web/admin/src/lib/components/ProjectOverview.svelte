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
  let responseCount: number | null = null;
  let loading = true;
  let error = '';

  $: fieldCount = draft?.schema.pages.reduce((total, page) => total + page.fields.length, 0) ?? 0;
  $: canEdit = user.role === 'admin' || user.role === 'designer';

  const shortcuts = [
    { id: 'builder', title: 'ساخت فرم', description: 'صفحه‌ها و فیلدهای فرم را ویرایش کنید.', icon: '✎' },
    { id: 'routes', title: 'مسیرها', description: 'منطق شرطی و پایان‌ها را مدیریت کنید.', icon: '↝' },
    { id: 'publish', title: 'انتشار', description: 'ظاهر فرم و نسخه‌های انتشار را ببینید.', icon: '↑' },
    { id: 'responses', title: 'پاسخ‌ها', description: 'پاسخ‌های ثبت‌شده را مرور و خروجی بگیرید.', icon: '▤' },
    { id: 'reports', title: 'گزارش‌ها', description: 'روندها و شاخص‌های فرم را تحلیل کنید.', icon: '⌁' },
    { id: 'ai', title: 'گفت‌وگو با داده', description: 'از دستیار فقط‌خواندنی دربارهٔ داده‌ها بپرسید.', icon: '✦' },
    { id: 'settings', title: 'تنظیمات', description: 'انتقال پروژه و رضایت تحلیل را تنظیم کنید.', icon: '⚙' }
  ] as const;

  onMount(load);

  async function load() {
    loading = true;
    error = '';
    try {
      [project, draft, versions] = await Promise.all([
        api.project(projectID),
        api.draft(projectID),
        api.versions(projectID)
      ]);
      try {
        responseCount = (await api.responses(projectID, { page: 1, page_size: 1 })).pagination.total;
      } catch {
        responseCount = null;
      }
    } catch (caught) {
      error = caught instanceof APIError ? caught.message : 'ارتباط با سرور برقرار نشد.';
    } finally {
      loading = false;
    }
  }

  function statusLabel(status: Project['status']): string {
    return { draft: 'پیش‌نویس', published: 'منتشرشده', closed: 'متوقف', trash: 'زباله‌دان' }[status];
  }
</script>

{#if loading}
  <div class="builder-loading">
    <div class="loader"></div>
    <span>در حال آماده‌سازی نمای کلی…</span>
  </div>
{:else if !project || !draft}
  <main class="startup-error embedded">
    <div class="status-orb error">!</div>
    <h1>نمای کلی باز نشد</h1>
    <p>{error}</p>
    <button class="button primary" type="button" onclick={load}>تلاش دوباره</button>
  </main>
{:else}
  <div class="builder-page data-page overview-page">
    <header class="builder-header">
      <div class="builder-title">
        <button class="icon-button ghost" type="button" aria-label="بازگشت به پروژه‌ها" onclick={() => onNavigate('/')}
          ><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 18 6-6-6-6" /></svg></button
        >
        <div>
          <span>پروژه / نمای کلی</span>
          <h1>{project.title}</h1>
        </div>
      </div>
      <span class={`badge ${project.status}`}>{statusLabel(project.status)}</span>
    </header>
    <ProjectTabs {projectID} active="overview" {onNavigate} />

    <main class="overview-content">
      <section class="overview-hero builder-panel">
        <div>
          <p class="eyebrow">وضعیت پروژه</p>
          <h2>{project.title}</h2>
          <p>آخرین تغییر {formatDateTime(project.updated_at)}</p>
        </div>
        <div class="overview-primary-actions">
          <button class="button primary" type="button" onclick={() => onNavigate(`/projects/${projectID}/builder`)}>
            {canEdit ? 'ادامهٔ ساخت فرم' : 'مشاهدهٔ فرم'}
          </button>
          {#if project.status === 'published' || project.status === 'closed'}
            <a class="button secondary" href={`/f/${project.slug}`} target="_blank" rel="noreferrer"
              >باز کردن فرم عمومی</a
            >
          {/if}
        </div>
      </section>

      <section class="overview-stats" aria-label="خلاصهٔ پروژه">
        <article>
          <span>صفحه</span><strong>{toPersianDigits(draft.schema.pages.length)}</strong><small>در پیش‌نویس فعلی</small>
        </article>
        <article><span>فیلد</span><strong>{toPersianDigits(fieldCount)}</strong><small>در همهٔ صفحه‌ها</small></article>
        <article>
          <span>نسخهٔ منتشرشده</span><strong>{toPersianDigits(versions.length)}</strong><small
            >snapshot تغییرناپذیر</small
          >
        </article>
        <article>
          <span>پاسخ نهایی</span><strong>{responseCount === null ? '—' : toPersianDigits(responseCount)}</strong><small
            >{responseCount === null ? 'شمارش در دسترس نیست' : 'بدون موارد حذف‌شده'}</small
          >
        </article>
      </section>

      <section>
        <div class="section-heading compact">
          <h3>بخش‌های پروژه</h3>
          <span>همهٔ ابزارهای این فرم در یک جا</span>
        </div>
        <div class="overview-shortcuts">
          {#each shortcuts as shortcut}
            <button type="button" onclick={() => onNavigate(`/projects/${projectID}/${shortcut.id}`)}>
              <span aria-hidden="true">{shortcut.icon}</span>
              <div><strong>{shortcut.title}</strong><small>{shortcut.description}</small></div>
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 18 6-6-6-6" /></svg>
            </button>
          {/each}
        </div>
      </section>
    </main>
  </div>
{/if}
