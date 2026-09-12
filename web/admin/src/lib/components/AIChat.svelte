<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { api, APIError } from '../api';
  import { formatDateTime } from '../persian-date';
  import type { AIConversation, AIMessage, AIProjectSettings, Project } from '../types';
  import ProjectTabs from './ProjectTabs.svelte';

  export let projectID: string;
  export let onNavigate: (path: string) => void;

  let project: Project | null = null;
  let settings: AIProjectSettings | null = null;
  let conversations: AIConversation[] = [];
  let active: AIConversation | null = null;
  let messages: AIMessage[] = [];
  let draft = '';
  let streamedText = '';
  let loading = true;
  let working = false;
  let error = '';
  let messageList: HTMLDivElement;

  onMount(load);

  async function load() {
    loading = true;
    error = '';
    try {
      [project, settings, conversations] = await Promise.all([
        api.project(projectID),
        api.aiProjectSettings(projectID),
        api.aiConversations(projectID)
      ]);
      if (conversations.length > 0) await selectConversation(conversations[0]);
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      loading = false;
    }
  }

  async function selectConversation(item: AIConversation) {
    if (working) return;
    active = item;
    streamedText = '';
    error = '';
    try {
      messages = await api.aiMessages(projectID, item.id);
      await scrollToEnd();
    } catch (caught) {
      error = messageOf(caught);
    }
  }

  async function createConversation() {
    if (!settings?.enabled || working) return;
    error = '';
    try {
      const created = await api.createAIConversation(projectID);
      conversations = [created, ...conversations];
      active = created;
      messages = [];
      draft = '';
    } catch (caught) {
      error = messageOf(caught);
    }
  }

  async function removeConversation(item: AIConversation) {
    if (working || !window.confirm(`گفت‌وگوی «${item.title}» حذف شود؟`)) return;
    error = '';
    try {
      await api.deleteAIConversation(projectID, item.id);
      conversations = conversations.filter((conversation) => conversation.id !== item.id);
      if (active?.id === item.id) {
        active = null;
        messages = [];
        if (conversations.length > 0) await selectConversation(conversations[0]);
      }
    } catch (caught) {
      error = messageOf(caught);
    }
  }

  async function send() {
    const content = draft.trim();
    if (!content || !active || working || content.length > 4000) return;
    working = true;
    error = '';
    draft = '';
    streamedText = '';
    const optimistic: AIMessage = {
      id: `local-${Date.now()}`,
      conversation_id: active.id,
      role: 'user',
      content,
      created_at: new Date().toISOString()
    };
    messages = [...messages, optimistic];
    await scrollToEnd();
    try {
      await api.streamAIMessage(projectID, active.id, content, async (delta) => {
        streamedText += delta;
        await scrollToEnd();
      });
      messages = await api.aiMessages(projectID, active.id);
      streamedText = '';
      conversations = await api.aiConversations(projectID);
      active = conversations.find((item) => item.id === active?.id) ?? active;
    } catch (caught) {
      messages = messages.filter((message) => message.id !== optimistic.id);
      draft = content;
      error = messageOf(caught);
    } finally {
      working = false;
      await scrollToEnd();
    }
  }

  async function scrollToEnd() {
    await tick();
    if (messageList) messageList.scrollTop = messageList.scrollHeight;
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      send();
    }
  }

  function messageOf(caught: unknown): string {
    if (caught instanceof APIError && ['ai_provider_error', 'ai_provider_timeout', 'ai_rate_limited'].includes(caught.code)) {
      return `${caught.message}؛ متن شما نگه داشته شد و بعداً می‌توانید دوباره ارسال کنید.`;
    }
    return caught instanceof APIError ? caught.message : 'ارتباط با سرور برقرار نشد.';
  }
</script>

{#if loading}
  <div class="builder-loading">
    <div class="loader"></div>
    <span>در حال آماده‌سازی گفت‌وگو…</span>
  </div>
{:else if !project || !settings}
  <main class="startup-error embedded">
    <div class="status-orb error">!</div>
    <h1>گفت‌وگو باز نشد</h1>
    <p>{error}</p>
    <button class="button primary" type="button" onclick={load}>تلاش دوباره</button>
  </main>
{:else}
  <div class="builder-page data-page ai-page">
    <header class="builder-header">
      <div class="builder-title">
        <button class="icon-button ghost" type="button" aria-label="بازگشت به پروژه‌ها" onclick={() => onNavigate('/')}
          ><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 18 6-6-6-6" /></svg></button
        >
        <div>
          <span>پروژه / تحلیل فقط‌خواندنی</span>
          <h1>{project.title}</h1>
        </div>
      </div>
      <span class="ai-privacy-badge"><span></span>داده‌ها فقط خوانده می‌شوند</span>
    </header>
    <ProjectTabs {projectID} active="ai" {onNavigate} />

    {#if error}<div class="builder-alert alert error" role="alert">{error}</div>{/if}

    {#if !settings.enabled || !settings.provider_configured}
      <main class="ai-empty builder-panel">
        <div class="ai-empty-icon" aria-hidden="true">✦</div>
        <h2>{!settings.enabled ? 'گفت‌وگو برای این پروژه خاموش است' : 'اتصال هوش مصنوعی آماده نیست'}</h2>
        <p>
          {!settings.enabled
            ? 'برای جلوگیری از ارسال ناخواستهٔ داده، این قابلیت باید جداگانه در تنظیمات همین پروژه فعال شود.'
            : 'مدیر سامانه باید اتصال provider را ذخیره و آزمون سازگاری را با موفقیت اجرا کند.'}
        </p>
        <button class="button primary" type="button" onclick={() => onNavigate(`/projects/${projectID}/settings`)}
          >رفتن به تنظیمات</button
        >
      </main>
    {:else}
      <main class="ai-workspace">
        <aside class="builder-panel ai-conversations">
          <div class="ai-sidebar-heading">
            <div>
              <p class="eyebrow">تاریخچهٔ شخصی</p>
              <h2>گفت‌وگوها</h2>
            </div>
            <button class="icon-button primary" type="button" aria-label="گفت‌وگوی تازه" onclick={createConversation}
              >＋</button
            >
          </div>
          <div class="ai-conversation-list">
            {#each conversations as conversation}
              <div class:active={active?.id === conversation.id} class="ai-conversation-row">
                <button type="button" onclick={() => selectConversation(conversation)}>
                  <strong>{conversation.title}</strong><small>{formatDateTime(conversation.updated_at)}</small>
                </button>
                <button
                  class="ai-delete"
                  type="button"
                  aria-label={`حذف ${conversation.title}`}
                  onclick={() => removeConversation(conversation)}>×</button
                >
              </div>
            {:else}
              <div class="ai-sidebar-empty">هنوز گفت‌وگویی ندارید.</div>
            {/each}
          </div>
          <div class="ai-consent-state" class:sampling={settings.text_sampling_enabled}>
            <span aria-hidden="true">{settings.text_sampling_enabled ? '!' : '✓'}</span>
            <p>
              {settings.text_sampling_enabled
                ? 'خواندن نمونهٔ متن با رضایت پروژه فعال است.'
                : 'فقط آمار تجمیعی؛ متن پاسخ‌ها ارسال نمی‌شود.'}
            </p>
          </div>
        </aside>

        <section class="builder-panel ai-chat-panel">
          {#if !active}
            <div class="ai-chat-empty">
              <div aria-hidden="true">✦</div>
              <h2>از داده‌های فرم بپرسید</h2>
              <p>دستیار برای ادعاهای آماری فقط از ابزارهای محدود و فقط‌خواندنی استفاده می‌کند.</p>
              <div class="ai-suggestions">
                <span>نرخ تکمیل چقدر است؟</span><span>گزینه‌های محبوب کدام‌اند؟</span><span
                  >روند پاسخ‌ها را خلاصه کن</span
                >
              </div>
              <button class="button primary" type="button" onclick={createConversation}>شروع گفت‌وگوی تازه</button>
            </div>
          {:else}
            <div class="ai-chat-heading">
              <div>
                <p class="eyebrow">دستیار داده</p>
                <h2>{active.title}</h2>
              </div>
              <span>حداکثر ۳۰ پیام اخیر در زمینه</span>
            </div>
            <div class="ai-messages" bind:this={messageList} aria-live="polite">
              {#each messages as message}
                <article
                  class:assistant={message.role === 'assistant'}
                  class:user={message.role === 'user'}
                  class="ai-message"
                >
                  <div class="ai-avatar" aria-hidden="true">{message.role === 'assistant' ? '✦' : 'ش'}</div>
                  <div>
                    <strong>{message.role === 'assistant' ? 'دستیار' : 'شما'}</strong>
                    <p>{message.content}</p>
                    {#if message.context_summary?.tools?.length}
                      <small>بر پایهٔ {message.context_summary.tools.length} پرس‌وجوی فقط‌خواندنی</small>
                    {/if}
                  </div>
                </article>
              {/each}
              {#if working}
                <article class="ai-message assistant pending">
                  <div class="ai-avatar" aria-hidden="true">✦</div>
                  <div>
                    <strong>دستیار</strong>{#if streamedText}<p>{streamedText}</p>{:else}<div class="ai-thinking">
                        <i></i><i></i><i></i>
                      </div>{/if}
                  </div>
                </article>
              {/if}
            </div>
            <form
              class="ai-composer"
              onsubmit={(event) => {
                event.preventDefault();
                send();
              }}
            >
              <textarea
                bind:value={draft}
                onkeydown={handleKeydown}
                maxlength="4000"
                rows="3"
                placeholder="مثلاً: نرخ تکمیل این ماه را با ماه قبل مقایسه کن…"
                disabled={working}
                aria-label="پیام شما"></textarea>
              <div>
                <small>Ctrl + Enter برای ارسال</small>
                <button class="button primary" type="submit" disabled={working || !draft.trim()}
                  >{working ? 'در حال تحلیل…' : 'ارسال'}</button
                >
              </div>
            </form>
          {/if}
        </section>
      </main>
    {/if}
  </div>
{/if}
