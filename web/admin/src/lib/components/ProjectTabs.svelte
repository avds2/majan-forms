<script lang="ts">
  import { onMount } from 'svelte';

  export let projectID: string;
  export let active: 'overview' | 'builder' | 'routes' | 'publish' | 'responses' | 'reports' | 'ai' | 'settings';
  export let onNavigate: (path: string) => void;

  let tabList: HTMLElement;

  const tabs = [
    { id: 'builder', label: 'ساخت فرم' },
    { id: 'routes', label: 'مسیرها' },
    { id: 'publish', label: 'انتشار' },
    { id: 'responses', label: 'پاسخ‌ها' },
    { id: 'reports', label: 'گزارش‌ها' },
    { id: 'ai', label: 'گفت‌وگو با داده' },
    { id: 'settings', label: 'تنظیمات' }
  ] as const;

  onMount(() => {
    tabList.querySelector<HTMLElement>('[aria-current="page"]')?.scrollIntoView({
      block: 'nearest',
      inline: 'center'
    });
  });
</script>

<nav class="project-tabs" aria-label="بخش‌های پروژه" bind:this={tabList}>
  <button
    class:active={active === 'overview'}
    type="button"
    aria-current={active === 'overview' ? 'page' : undefined}
    onclick={() => onNavigate(`/projects/${projectID}/overview`)}>نمای کلی</button
  >
  {#each tabs as tab}
    <button
      class:active={active === tab.id}
      type="button"
      aria-current={active === tab.id ? 'page' : undefined}
      onclick={() => onNavigate(`/projects/${projectID}/${tab.id}`)}>{tab.label}</button
    >
  {/each}
</nav>
