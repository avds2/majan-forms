<script lang="ts">
  import { onMount } from 'svelte';
  import type { User } from '../types';

  export let user: User;
  export let activePath: string;
  export let onNavigate: (path: string) => void;
  export let onLogout: () => void;

  let mobileOpen = false;
  let collapsed = false;

  onMount(() => {
    collapsed = window.localStorage.getItem('forms-sidebar-collapsed') === 'true';
  });

  const items = [
    { path: '/', label: 'پروژه‌ها', icon: 'projects' },
    { path: '/users', label: 'کاربران', icon: 'users', admin: true },
    { path: '/system', label: 'سلامت سیستم', icon: 'health', admin: true },
    { path: '/trash', label: 'زباله‌دان', icon: 'trash', admin: true }
  ];

  function navigate(path: string) {
    mobileOpen = false;
    onNavigate(path);
  }

  function toggleCollapsed() {
    collapsed = !collapsed;
    window.localStorage.setItem('forms-sidebar-collapsed', String(collapsed));
  }

  function roleLabel() {
    return { admin: 'مدیر', designer: 'طراح فرم', viewer: 'مشاهده‌گر' }[user.role];
  }
</script>

<div class:nav-collapsed={collapsed} class="app-shell">
  <header class="mobile-header">
    <button class="icon-button" type="button" aria-label="باز کردن فهرست" onclick={() => (mobileOpen = !mobileOpen)}>
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M4 12h16M4 17h16" /></svg>
    </button>
    <div class="wordmark">
      <div class="brand-mark" aria-hidden="true"><span></span><span></span><span></span></div>
      <strong>فرم‌ها</strong>
    </div>
  </header>
  {#if mobileOpen}<button class="nav-scrim" aria-label="بستن فهرست" onclick={() => (mobileOpen = false)}></button>{/if}
  <aside class:open={mobileOpen} class:collapsed class="side-nav">
    <div class="wordmark">
      <div class="brand-mark" aria-hidden="true"><span></span><span></span><span></span></div>
      <strong>فرم‌ها</strong>
    </div>
    <button
      class="desktop-nav-toggle"
      type="button"
      aria-label={collapsed ? 'باز کردن پنل کناری' : 'جمع کردن پنل کناری'}
      aria-expanded={!collapsed}
      title={collapsed ? 'باز کردن پنل کناری' : 'جمع کردن پنل کناری'}
      onclick={toggleCollapsed}
    >
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d={collapsed ? 'm9 18 6-6-6-6' : 'm15 18-6-6 6-6'} /></svg>
    </button>
    <nav aria-label="ناوبری اصلی">
      <p class="nav-label">فضای کاری</p>
      {#each items as item}
        {#if !item.admin || user.role === 'admin'}
          <button
            class:active={activePath === item.path}
            type="button"
            title={collapsed ? item.label : undefined}
            onclick={() => navigate(item.path)}
          >
            {#if item.icon === 'projects'}<svg viewBox="0 0 24 24" aria-hidden="true"
                ><path d="M4 6.5h6l2 2h8v10H4z" /></svg
              >{/if}
            {#if item.icon === 'users'}<svg viewBox="0 0 24 24" aria-hidden="true"
                ><path
                  d="M16 20v-2a4 4 0 0 0-4-4H7a4 4 0 0 0-4 4v2m6.5-10a4 4 0 1 0 0-8 4 4 0 0 0 0 8Zm7.5 1a3 3 0 1 0 0-6m4 15v-1a4 4 0 0 0-3-3.87"
                /></svg
              >{/if}
            {#if item.icon === 'trash'}<svg viewBox="0 0 24 24" aria-hidden="true"
                ><path d="M4 7h16m-11 4v6m6-6v6M9 4h6l1 3H8zm-3 3 1 14h10l1-14" /></svg
              >{/if}
            {#if item.icon === 'health'}<svg viewBox="0 0 24 24" aria-hidden="true"
                ><path
                  d="M4 12h3l2-5 4 10 2-5h5M5 4h14a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z"
                /></svg
              >{/if}
            <span>{item.label}</span>
          </button>
        {/if}
      {/each}
    </nav>
    <div class="side-spacer"></div>
    <div class="user-compact">
      <div class="avatar">{user.display_name.slice(0, 1)}</div>
      <div><strong>{user.display_name}</strong><span>{roleLabel()}</span></div>
      <button class="icon-button ghost" type="button" aria-label="خروج" onclick={onLogout}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M10 17l5-5-5-5m5 5H3m11-9h6v18h-6" /></svg>
      </button>
    </div>
  </aside>
  <main class="main-surface"><slot /></main>
</div>
