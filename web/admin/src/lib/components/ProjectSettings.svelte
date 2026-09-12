<script lang="ts">
  import { onMount } from 'svelte';
  import { api, APIError } from '../api';
  import { formatDateTime, toPersianDigits } from '../persian-date';
  import type {
    AIProjectSettings,
    AIProviderCompatibility,
    AIProviderSettings,
    FormVersion,
    Project,
    ProjectPackageImportResult,
    ProjectPackagePreview,
    User
  } from '../types';
  import ProjectTabs from './ProjectTabs.svelte';

  export let user: User;
  export let projectID: string;
  export let onNavigate: (path: string) => void;

  let project: Project | null = null;
  let versions: FormVersion[] = [];
  let selectedVersions: number[] = [];
  let packageFile: File | null = null;
  let preview: ProjectPackagePreview | null = null;
  let imported: ProjectPackageImportResult | null = null;
  let importTitle = '';
  let importSlug = '';
  let loading = true;
  let previewing = false;
  let importing = false;
  let error = '';
  let aiSettings: AIProjectSettings | null = null;
  let provider: AIProviderSettings | null = null;
  let providerForm = {
    base_url: 'https://api.openai.com/v1',
    api_mode: 'responses' as 'responses' | 'chat_completions',
    model: '',
    timeout_seconds: 60,
    max_output_tokens: 2048,
    allow_private_endpoint: false,
    api_key: ''
  };
  let compatibility: AIProviderCompatibility | null = null;
  let savingProvider = false;
  let testingProvider = false;
  let savingAIProject = false;

  $: canImport = user.role === 'admin' || user.role === 'designer';
  $: canEditAI = user.role === 'admin' || user.role === 'designer';
  $: exportReady = versions.length === 0 || selectedVersions.length > 0;
  $: exportURL = api.projectPackageURL(
    projectID,
    versions.length > 0 && selectedVersions.length < versions.length ? selectedVersions : undefined
  );

  onMount(load);

  async function load() {
    loading = true;
    error = '';
    try {
      [project, versions, aiSettings] = await Promise.all([
        api.project(projectID),
        api.versions(projectID),
        api.aiProjectSettings(projectID)
      ]);
      if (user.role === 'admin') {
        provider = await api.aiProvider();
        providerForm = {
          base_url: provider.base_url,
          api_mode: provider.api_mode,
          model: provider.model,
          timeout_seconds: provider.timeout_seconds,
          max_output_tokens: provider.max_output_tokens,
          allow_private_endpoint: provider.allow_private_endpoint,
          api_key: ''
        };
      }
      selectedVersions = versions.map((version) => version.version_number);
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      loading = false;
    }
  }

  async function saveProvider() {
    if (user.role !== 'admin') return;
    savingProvider = true;
    compatibility = null;
    error = '';
    try {
      provider = await api.saveAIProvider({ ...providerForm, api_key: providerForm.api_key || undefined });
      providerForm.api_key = '';
      if (aiSettings) aiSettings = { ...aiSettings, provider_configured: false };
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      savingProvider = false;
    }
  }

  async function testProvider() {
    if (user.role !== 'admin') return;
    testingProvider = true;
    compatibility = null;
    error = '';
    try {
      compatibility = await api.testAIProvider();
      provider = await api.aiProvider();
      if (aiSettings) aiSettings = { ...aiSettings, provider_configured: true };
    } catch (caught) {
      if (provider) provider = { ...provider, compatible: false, compatibility_tested_at: undefined };
      if (aiSettings) aiSettings = { ...aiSettings, provider_configured: false };
      error = messageOf(caught);
    } finally {
      testingProvider = false;
    }
  }

  async function saveProjectAI() {
    if (!aiSettings || !canEditAI) return;
    savingAIProject = true;
    error = '';
    try {
      aiSettings = await api.saveAIProjectSettings(
        projectID,
        aiSettings.enabled,
        aiSettings.enabled && aiSettings.text_sampling_enabled
      );
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      savingAIProject = false;
    }
  }

  function toggleVersion(versionNumber: number, checked: boolean) {
    selectedVersions = checked
      ? [...selectedVersions, versionNumber].sort((left, right) => left - right)
      : selectedVersions.filter((number) => number !== versionNumber);
  }

  async function choosePackage(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    packageFile = input.files?.[0] ?? null;
    preview = null;
    imported = null;
    error = '';
    if (!packageFile) return;
    if (packageFile.size > 32 * 1024 * 1024) {
      error = 'حجم بسته بیشتر از ۳۲ مگابایت است.';
      packageFile = null;
      return;
    }
    previewing = true;
    try {
      preview = await api.previewProjectPackage(packageFile);
      importTitle = `${preview.source_title} — کپی`;
      importSlug = suggestedSlug(preview.source_slug);
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      previewing = false;
    }
  }

  async function importPackage() {
    if (!packageFile || !preview || !canImport) return;
    importing = true;
    imported = null;
    error = '';
    try {
      imported = await api.importProjectPackage(packageFile, importTitle, importSlug);
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      importing = false;
    }
  }

  function suggestedSlug(source: string): string {
    const suffix = '-copy';
    const base = source.replace(/-+$/g, '').slice(0, 63 - suffix.length);
    return `${base}${suffix}`;
  }

  function formatBytes(bytes: number): string {
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
    <span>در حال آماده‌سازی انتقال پروژه…</span>
  </div>
{:else if !project}
  <main class="startup-error embedded">
    <div class="status-orb error">!</div>
    <h1>تنظیمات باز نشد</h1>
    <p>{error}</p>
    <button class="button primary" type="button" onclick={load}>تلاش دوباره</button>
  </main>
{:else}
  <div class="builder-page data-page package-page">
    <header class="builder-header">
      <div class="builder-title">
        <button class="icon-button ghost" type="button" aria-label="بازگشت به پروژه‌ها" onclick={() => onNavigate('/')}
          ><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 18 6-6-6-6" /></svg></button
        >
        <div>
          <span>پروژه / تنظیمات انتقال</span>
          <h1>{project.title}</h1>
        </div>
      </div>
      <span class="status-pill {project.status}"
        >{project.status === 'published' ? 'منتشرشده' : project.status === 'closed' ? 'متوقف' : 'پیش‌نویس'}</span
      >
    </header>
    <ProjectTabs {projectID} active="settings" {onNavigate} />

    {#if error}<div class="builder-alert alert error" role="alert">{error}</div>{/if}

    <main class="settings-content">
      <section class="builder-panel ai-settings-card project-ai-card">
        <div class="package-card-heading">
          <span class="package-icon ai" aria-hidden="true">✦</span>
          <div>
            <p class="eyebrow">رضایت مستقل هر پروژه</p>
            <h2>گفت‌وگو با داده</h2>
          </div>
        </div>
        <p class="package-description">
          این قابلیت مستقل از اتصال سراسری است و به‌صورت پیش‌فرض خاموش می‌ماند. ابزارها فقط خواندنی‌اند و هیچ فرم یا
          پاسخی را تغییر نمی‌دهند.
        </p>
        {#if aiSettings}
          <div class="ai-setting-options">
            <label class="setting-toggle">
              <input type="checkbox" bind:checked={aiSettings.enabled} disabled={!canEditAI || savingAIProject} />
              <span
                ><strong>فعال‌سازی تحلیل برای این پروژه</strong><small
                  >آمار تجمیعی، ساختار فرم و روندها در دسترس دستیار قرار می‌گیرد.</small
                ></span
              >
            </label>
            <label class="setting-toggle danger-consent" class:disabled={!aiSettings.enabled}>
              <input
                type="checkbox"
                bind:checked={aiSettings.text_sampling_enabled}
                disabled={!canEditAI || !aiSettings.enabled || savingAIProject}
              />
              <span
                ><strong>اجازهٔ صریح برای نمونه‌خوانی پاسخ‌های متنی</strong><small
                  >ممکن است متن خام پاسخ‌دهنده برای تحلیل به provider ارسال شود؛ حداکثر ۱۰ نمونه در هر درخواست.</small
                ></span
              >
            </label>
          </div>
          <div class="ai-settings-actions">
            <span class:ready={aiSettings.provider_configured} class="connection-state">
              {aiSettings.provider_configured ? 'اتصال provider آماده است' : 'اتصال provider هنوز کامل نیست'}
            </span>
            {#if canEditAI}<button
                class="button primary"
                type="button"
                onclick={saveProjectAI}
                disabled={savingAIProject}>{savingAIProject ? 'در حال ذخیره…' : 'ذخیرهٔ رضایت پروژه'}</button
              >{/if}
          </div>
        {/if}
      </section>

      {#if user.role === 'admin'}
        <section class="builder-panel ai-settings-card provider-card">
          <div class="package-card-heading">
            <span class="package-icon provider" aria-hidden="true">⌁</span>
            <div>
              <p class="eyebrow">فقط مدیر سامانه</p>
              <h2>اتصال provider سازگار</h2>
            </div>
          </div>
          <p class="package-description">
            کلید API فقط در فایل رمز با دسترسی محدود ذخیره می‌شود؛ در پایگاه داده یا پاسخ API برگردانده نمی‌شود. ذخیرهٔ
            تنظیمات هیچ درخواست آزمایشی نمی‌فرستد.
          </p>
          <form
            class="provider-form"
            onsubmit={(event) => {
              event.preventDefault();
              saveProvider();
            }}
          >
            <div class="field-group wide">
              <label for="ai-base-url">نشانی پایه</label>
              <input id="ai-base-url" dir="ltr" type="url" bind:value={providerForm.base_url} required />
              <small>HTTPS الزامی است؛ HTTP فقط برای loopback و با تأیید زیر پذیرفته می‌شود.</small>
            </div>
            <div class="field-group">
              <label for="ai-mode">قرارداد API</label>
              <select id="ai-mode" bind:value={providerForm.api_mode}>
                <option value="responses">Responses API</option><option value="chat_completions"
                  >Chat Completions</option
                >
              </select>
            </div>
            <div class="field-group">
              <label for="ai-model">نام مدل</label><input
                id="ai-model"
                dir="ltr"
                bind:value={providerForm.model}
                required
                maxlength="200"
                placeholder="gpt-5-mini"
              />
            </div>
            <div class="field-group">
              <label for="ai-timeout">مهلت پاسخ (ثانیه)</label><input
                id="ai-timeout"
                type="number"
                min="5"
                max="180"
                bind:value={providerForm.timeout_seconds}
                required
              />
            </div>
            <div class="field-group">
              <label for="ai-tokens">سقف توکن خروجی</label><input
                id="ai-tokens"
                type="number"
                min="128"
                max="32768"
                bind:value={providerForm.max_output_tokens}
                required
              />
            </div>
            <div class="field-group wide">
              <label for="ai-key">کلید API</label>
              <input
                id="ai-key"
                dir="ltr"
                type="password"
                bind:value={providerForm.api_key}
                autocomplete="new-password"
                placeholder={provider?.has_api_key
                  ? 'کلید ذخیره شده؛ برای حفظ آن خالی بگذارید'
                  : 'کلید API را وارد کنید'}
              />
            </div>
            <label class="private-endpoint-consent wide">
              <input type="checkbox" bind:checked={providerForm.allow_private_endpoint} />
              <span
                ><strong>آگاهانه اجازهٔ endpoint خصوصی/محلی را می‌دهم</strong><small
                  >این گزینه حفاظت SSRF برای شبکه‌های خصوصی را باز می‌کند؛ تغییر مسیر همچنان مسدود است.</small
                ></span
              >
            </label>
            <div class="provider-actions wide">
              <button class="button primary" type="submit" disabled={savingProvider}
                >{savingProvider ? 'در حال ذخیره…' : 'ذخیرهٔ اتصال'}</button
              >
              <button
                class="button secondary"
                type="button"
                onclick={testProvider}
                disabled={testingProvider || savingProvider || !provider?.has_api_key || !provider?.model}
                >{testingProvider ? 'در حال آزمون…' : 'آزمون streaming و ابزارها'}</button
              >
              {#if compatibility}
                <span class="compatibility-ok">✓ سازگار · {toPersianDigits(compatibility.latency_ms)} میلی‌ثانیه</span>
              {:else if provider && !provider.model}
                <span class="connection-state">برای آزمون اتصال، ابتدا نام مدل را ذخیره کنید.</span>
              {:else if provider && !provider.has_api_key}
                <span class="connection-state">برای آزمون اتصال، ابتدا کلید API را ذخیره کنید.</span>
              {:else if provider && !provider.compatible}
                <span class="connection-state">اتصال آماده نیست؛ پس از در دسترس‌شدن provider دوباره آزمون کنید.</span>
              {/if}
            </div>
          </form>
        </section>
      {/if}

      <div class="package-content">
        <section class="builder-panel package-card">
          <div class="package-card-heading">
            <span class="package-icon" aria-hidden="true"
              ><svg viewBox="0 0 24 24"><path d="M12 3v12m0 0 4-4m-4 4-4-4M5 19h14" /></svg></span
            >
            <div>
              <p class="eyebrow">خروجی قابل‌انتقال</p>
              <h2>دریافت بستهٔ پروژه</h2>
            </div>
          </div>
          <p class="package-description">
            پیش‌نویس، مسیرها، ظاهر فرم و نسخه‌های انتخابی در یک فایل ZIP نسخه‌دار قرار می‌گیرند. پاسخ‌ها و فایل‌های
            پاسخ‌دهندگان هرگز وارد این بسته نمی‌شوند.
          </p>

          {#if versions.length > 0}
            <fieldset class="version-selector">
              <legend>نسخه‌های منتشرشده</legend>
              {#each versions as version}
                <label>
                  <input
                    type="checkbox"
                    checked={selectedVersions.includes(version.version_number)}
                    onchange={(event) => toggleVersion(version.version_number, event.currentTarget.checked)}
                  />
                  <span
                    ><strong>نسخهٔ {toPersianDigits(version.version_number)}</strong><small
                      >{formatDateTime(version.published_at)}</small
                    ></span
                  >
                </label>
              {/each}
            </fieldset>
          {:else}
            <div class="package-note">این پروژه هنوز نسخهٔ منتشرشده ندارد؛ بسته فقط پیش‌نویس فعلی را خواهد داشت.</div>
          {/if}

          {#if exportReady}
            <a class="button primary package-download" href={exportURL} download>
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3v12m0 0 4-4m-4 4-4-4M5 19h14" /></svg>
              دریافت project-package-v1.zip
            </a>
          {:else}
            <button class="button primary package-download" type="button" disabled>دست‌کم یک نسخه را انتخاب کنید</button
            >
          {/if}
        </section>

        <section class="builder-panel package-card import-card">
          <div class="package-card-heading">
            <span class="package-icon import" aria-hidden="true"
              ><svg viewBox="0 0 24 24"><path d="M12 15V3m0 0 4 4m-4-4L8 7M5 19h14" /></svg></span
            >
            <div>
              <p class="eyebrow">ورود کنترل‌شده</p>
              <h2>ساخت پروژه از بسته</h2>
            </div>
          </div>
          {#if canImport}
            <label class="package-drop" class:busy={previewing}>
              <input
                type="file"
                accept=".zip,application/zip"
                onchange={choosePackage}
                disabled={previewing || importing}
              />
              {#if previewing}
                <div class="loader"></div>
                <strong>در حال بررسی checksum و ساختار…</strong>
              {:else}
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 16V4m0 0 4 4m-4-4L8 8M4 18h16v3H4z" /></svg>
                <strong>{packageFile ? packageFile.name : 'فایل ZIP بستهٔ پروژه را انتخاب کنید'}</strong>
                <small
                  >{packageFile
                    ? formatBytes(packageFile.size)
                    : 'حداکثر ۳۲ مگابایت؛ فایل پیش از ورود فقط پیش‌نمایش می‌شود.'}</small
                >
              {/if}
            </label>
          {:else}
            <div class="package-note">
              نقش شما فقط اجازهٔ دریافت بسته را دارد؛ ورود پروژه برای مدیر و طراح فعال است.
            </div>
          {/if}

          {#if preview}
            <div class="package-preview" aria-live="polite">
              <div class="verified-line">
                <span aria-hidden="true">✓</span><strong>بسته معتبر و آمادهٔ ورود است</strong>
              </div>
              <div class="package-facts">
                <div><span>عنوان مبدأ</span><strong>{preview.source_title}</strong></div>
                <div><span>ساخته‌شده در</span><strong>{formatDateTime(preview.created_at)}</strong></div>
                <div>
                  <span>صفحه / فیلد</span><strong
                    >{toPersianDigits(preview.page_count)} / {toPersianDigits(preview.field_count)}</strong
                  >
                </div>
                <div>
                  <span>نسخه / دارایی</span><strong
                    >{toPersianDigits(preview.version_count)} / {toPersianDigits(preview.asset_count)}</strong
                  >
                </div>
              </div>
              <p class="checksum-line"><span>SHA-256</span><code dir="ltr">{preview.package_sha256}</code></p>
              <div class="safe-import-note">
                شناسهٔ پروژه و نسخه‌ها تازه ساخته می‌شود؛ شناسه‌های داخلی فیلد، صفحه و مسیر ثابت می‌ماند. پروژه با وضعیت
                «پیش‌نویس» و بدون پاسخ ساخته خواهد شد.
              </div>
              <form
                class="import-fields"
                onsubmit={(event) => {
                  event.preventDefault();
                  importPackage();
                }}
              >
                <div class="field-group">
                  <label for="import-title">عنوان پروژهٔ تازه</label><input
                    id="import-title"
                    bind:value={importTitle}
                    maxlength="200"
                    required
                  />
                </div>
                <div class="field-group">
                  <label for="import-slug">نشانی تازه</label>
                  <div class="input-affix ltr">
                    <span>/f/</span><input
                      id="import-slug"
                      dir="ltr"
                      bind:value={importSlug}
                      pattern={'[a-z0-9][a-z0-9-]{1,61}[a-z0-9]'}
                      required
                    />
                  </div>
                </div>
                <button class="button primary" type="submit" disabled={importing}
                  >{importing ? 'در حال ساخت پروژه…' : 'تأیید و ساخت پروژهٔ تازه'}</button
                >
              </form>
            </div>
          {/if}

          {#if imported}
            <div class="import-success" role="status">
              <span aria-hidden="true">✓</span>
              <div>
                <strong>پروژه با موفقیت و بدون پاسخ وارد شد.</strong>
                <small>{toPersianDigits(imported.imported_versions)} نسخهٔ تاریخی منتقل شد.</small>
              </div>
              <button
                class="button secondary"
                type="button"
                onclick={() => onNavigate(`/projects/${imported!.project.id}/builder`)}>باز کردن پروژه</button
              >
            </div>
          {/if}
        </section>
      </div>
    </main>
  </div>
{/if}
