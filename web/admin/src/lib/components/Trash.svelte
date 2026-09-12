<script lang="ts">
  import { onMount } from 'svelte';
  import { api, APIError } from '../api';
  import { formatDateTime, toPersianDigits } from '../persian-date';
  import type { Project } from '../types';

  export let onNavigate: (path: string) => void;

  let projects: Project[] = [];
  let loading = true;
  let error = '';
  let restoringID = '';

  onMount(load);

  async function load() {
    loading = true;
    error = '';
    try {
      projects = await api.projects(true);
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      loading = false;
    }
  }

  async function restore(project: Project) {
    restoringID = project.id;
    error = '';
    try {
      await api.restoreProject(project.id);
      projects = projects.filter((candidate) => candidate.id !== project.id);
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      restoringID = '';
    }
  }

  function messageOf(caught: unknown): string {
    return caught instanceof APIError ? caught.message : 'ارتباط با سرور برقرار نشد.';
  }
</script>

<div class="page-container trash-page">
  <header class="page-header">
    <div>
      <p class="eyebrow">مدیریت سازمان</p>
      <h1>زباله‌دان</h1>
      <p>پروژه‌های حذف‌شده اینجا قابل‌بازیابی‌اند؛ حذف دائم هنوز فعال نیست.</p>
    </div>
  </header>

  {#if error}<div class="alert error" role="alert">
      {error}<button type="button" onclick={load}>تلاش دوباره</button>
    </div>{/if}

  {#if loading}
    <div class="table-loading">
      <div class="loader"></div>
      <span>در حال دریافت زباله‌دان…</span>
    </div>
  {:else if projects.length === 0}
    <section class="empty-state compact">
      <div class="empty-icon">
        <svg viewBox="0 0 24 24" aria-hidden="true"
          ><path d="M4 7h16m-11 4v6m6-6v6M9 4h6l1 3H8zm-3 3 1 14h10l1-14" /></svg
        >
      </div>
      <h2>زباله‌دان خالی است</h2>
      <p>پروژه‌ای برای بازیابی وجود ندارد.</p>
      <button class="button secondary" type="button" onclick={() => onNavigate('/')}>بازگشت به پروژه‌ها</button>
    </section>
  {:else}
    <div class="section-heading">
      <h2>پروژه‌های حذف‌شده</h2>
      <span>{toPersianDigits(projects.length)} مورد</span>
    </div>
    <div class="trash-list">
      {#each projects as project}
        <article>
          <div class="project-icon muted-icon">
            <svg viewBox="0 0 24 24" aria-hidden="true"
              ><path d="M8 7h8M8 11h8M8 15h5M6 3h12a2 2 0 0 1 2 2v14H4V5a2 2 0 0 1 2-2Z" /></svg
            >
          </div>
          <div>
            <h3>{project.title}</h3>
            <p class="ltr-slug">/f/{project.slug}</p>
            <small>انتقال در {formatDateTime(project.deleted_at)}</small>
          </div>
          <button
            class="button secondary"
            type="button"
            disabled={restoringID === project.id}
            onclick={() => restore(project)}
          >
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 8v5h5M5 12a7 7 0 1 0 2-5" /></svg>
            {restoringID === project.id ? 'در حال بازیابی…' : 'بازیابی'}
          </button>
        </article>
      {/each}
    </div>
  {/if}
</div>
