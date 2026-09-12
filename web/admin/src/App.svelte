<script lang="ts">
  import { onMount } from 'svelte';
  import { api, APIError } from './lib/api';
  import { setDisplayTimezone } from './lib/persian-date';
  import type { User } from './lib/types';
  import Login from './lib/components/Login.svelte';
  import Shell from './lib/components/Shell.svelte';
  import Dashboard from './lib/components/Dashboard.svelte';
  import ProjectOverview from './lib/components/ProjectOverview.svelte';
  import Users from './lib/components/Users.svelte';
  import Trash from './lib/components/Trash.svelte';
  import SystemHealth from './lib/components/SystemHealth.svelte';
  import Builder from './lib/components/Builder.svelte';
  import Publish from './lib/components/Publish.svelte';

  let user: User | null = null;
  let loading = true;
  let startupError = '';
  let pathname = window.location.pathname;
  type ProjectSection = 'overview' | 'builder' | 'routes' | 'publish' | 'responses' | 'reports' | 'ai' | 'settings';
  let route: { id: string; section: ProjectSection } | null = null;
  let activePath = '/';
  let RoutesComponent: typeof import('./lib/components/Routes.svelte').default | null = null;
  let routesLoading = false;
  let ResponsesComponent: typeof import('./lib/components/Responses.svelte').default | null = null;
  let responsesLoading = false;
  let ReportsComponent: typeof import('./lib/components/Reports.svelte').default | null = null;
  let reportsLoading = false;
  let SettingsComponent: typeof import('./lib/components/ProjectSettings.svelte').default | null = null;
  let settingsLoading = false;
  let AIChatComponent: typeof import('./lib/components/AIChat.svelte').default | null = null;
  let aiChatLoading = false;

  $: route = projectRoute(pathname);
  $: activePath = activeNavigation(pathname);
  $: if (route?.section === 'routes' && !RoutesComponent && !routesLoading) loadRoutes();
  $: if (route?.section === 'responses' && !ResponsesComponent && !responsesLoading) loadResponses();
  $: if (route?.section === 'reports' && !ReportsComponent && !reportsLoading) loadReports();
  $: if (route?.section === 'settings' && !SettingsComponent && !settingsLoading) loadSettings();
  $: if (route?.section === 'ai' && !AIChatComponent && !aiChatLoading) loadAIChat();

  onMount(() => {
    const popstate = () => (pathname = window.location.pathname);
    window.addEventListener('popstate', popstate);
    boot();
    return () => window.removeEventListener('popstate', popstate);
  });

  async function boot() {
    loading = true;
    startupError = '';
    try {
      const runtimeConfig = await api.runtimeConfig();
      setDisplayTimezone(runtimeConfig.timezone);
      user = await api.me();
      if (pathname === '/login') navigate('/', true);
    } catch (caught) {
      if (caught instanceof APIError && caught.status === 401) {
        user = null;
      } else {
        startupError = 'ارتباط با سرور برقرار نشد. وضعیت سرویس را بررسی کنید.';
      }
    } finally {
      loading = false;
    }
  }

  function navigate(path: string, replace = false) {
    if (replace) window.history.replaceState({}, '', path);
    else window.history.pushState({}, '', path);
    pathname = path;
    window.scrollTo({ top: 0, behavior: 'smooth' });
  }

  async function logout() {
    try {
      await api.logout();
    } finally {
      user = null;
      navigate('/login', true);
    }
  }

  function activeNavigation(path: string): string {
    if (path === '/users') return '/users';
    if (path === '/trash') return '/trash';
    if (path === '/system') return '/system';
    return '/';
  }

  function projectRoute(path: string): { id: string; section: ProjectSection } | null {
    const match = /^\/projects\/([^/]+)\/(overview|builder|routes|publish|responses|reports|ai|settings)$/.exec(path);
    return match
      ? {
          id: decodeURIComponent(match[1]),
          section: match[2] as ProjectSection
        }
      : null;
  }

  async function loadRoutes() {
    routesLoading = true;
    try {
      RoutesComponent = (await import('./lib/components/Routes.svelte')).default;
    } finally {
      routesLoading = false;
    }
  }

  async function loadResponses() {
    responsesLoading = true;
    try {
      ResponsesComponent = (await import('./lib/components/Responses.svelte')).default;
    } finally {
      responsesLoading = false;
    }
  }

  async function loadReports() {
    reportsLoading = true;
    try {
      ReportsComponent = (await import('./lib/components/Reports.svelte')).default;
    } finally {
      reportsLoading = false;
    }
  }

  async function loadSettings() {
    settingsLoading = true;
    try {
      SettingsComponent = (await import('./lib/components/ProjectSettings.svelte')).default;
    } finally {
      settingsLoading = false;
    }
  }

  async function loadAIChat() {
    aiChatLoading = true;
    try {
      AIChatComponent = (await import('./lib/components/AIChat.svelte')).default;
    } finally {
      aiChatLoading = false;
    }
  }
</script>

{#if loading}
  <div class="startup" aria-live="polite">
    <div class="loader"></div>
    <span>در حال آماده‌سازی پنل…</span>
  </div>
{:else if startupError}
  <main class="startup-error">
    <div class="status-orb error">!</div>
    <h1>پنل در دسترس نیست</h1>
    <p>{startupError}</p>
    <button class="button primary" type="button" onclick={boot}>تلاش دوباره</button>
  </main>
{:else if !user}
  <Login
    onLoggedIn={(loggedIn) => {
      user = loggedIn;
      navigate('/', true);
    }}
  />
{:else}
  <Shell {user} {activePath} onNavigate={navigate} onLogout={logout}>
    {#if route?.section === 'overview'}
      <ProjectOverview {user} projectID={route.id} onNavigate={navigate} />
    {:else if route?.section === 'builder'}
      <Builder {user} projectID={route.id} onNavigate={navigate} />
    {:else if route?.section === 'routes'}
      {#if RoutesComponent}<RoutesComponent {user} projectID={route.id} onNavigate={navigate} />{:else}<div
          class="builder-loading"
        >
          <div class="loader"></div>
          <span>در حال بارگذاری بوم مسیرها…</span>
        </div>{/if}
    {:else if route?.section === 'publish'}
      <Publish {user} projectID={route.id} onNavigate={navigate} />
    {:else if route?.section === 'responses'}
      {#if ResponsesComponent}<ResponsesComponent {user} projectID={route.id} onNavigate={navigate} />{:else}<div
          class="builder-loading"
        >
          <div class="loader"></div>
          <span>در حال بارگذاری پاسخ‌ها…</span>
        </div>{/if}
    {:else if route?.section === 'reports'}
      {#if ReportsComponent}<ReportsComponent projectID={route.id} onNavigate={navigate} />{:else}<div
          class="builder-loading"
        >
          <div class="loader"></div>
          <span>در حال بارگذاری گزارش‌ها…</span>
        </div>{/if}
    {:else if route?.section === 'ai'}
      {#if AIChatComponent}<AIChatComponent projectID={route.id} onNavigate={navigate} />{:else}<div
          class="builder-loading"
        >
          <div class="loader"></div>
          <span>در حال آماده‌سازی گفت‌وگو با داده…</span>
        </div>{/if}
    {:else if route?.section === 'settings'}
      {#if SettingsComponent}<SettingsComponent {user} projectID={route.id} onNavigate={navigate} />{:else}<div
          class="builder-loading"
        >
          <div class="loader"></div>
          <span>در حال بارگذاری تنظیمات انتقال…</span>
        </div>{/if}
    {:else if pathname === '/users' && user.role === 'admin'}
      <Users {user} />
    {:else if pathname === '/trash' && user.role === 'admin'}
      <Trash onNavigate={navigate} />
    {:else if pathname === '/system' && user.role === 'admin'}
      <SystemHealth />
    {:else}
      <Dashboard {user} onNavigate={navigate} />
    {/if}
  </Shell>
{/if}
