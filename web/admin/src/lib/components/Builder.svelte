<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, APIError } from '../api';
  import { formatDateTime } from '../persian-date';
  import type { Draft, FieldType, FormField, FormPage, FormSchema, Project, User } from '../types';
  import DatePicker from './DatePicker.svelte';
  import Modal from './Modal.svelte';
  import ProjectTabs from './ProjectTabs.svelte';

  export let user: User;
  export let projectID: string;
  export let onNavigate: (path: string) => void;

  type SaveStatus = 'saved' | 'dirty' | 'saving' | 'error' | 'conflict';

  const catalog: Array<{ type: FieldType; label: string; mark: string }> = [
    { type: 'short_text', label: 'متن کوتاه', mark: 'ت' },
    { type: 'long_text', label: 'متن بلند', mark: '¶' },
    { type: 'email', label: 'ایمیل', mark: '@' },
    { type: 'phone', label: 'تلفن', mark: 'تـ' },
    { type: 'url', label: 'نشانی وب', mark: '↗' },
    { type: 'full_name', label: 'نام کامل', mark: 'ن' },
    { type: 'address', label: 'نشانی پستی', mark: '⌂' },
    { type: 'national_id', label: 'کد ملی', mark: 'م' },
    { type: 'number', label: 'عدد', mark: '۱۲' },
    { type: 'money', label: 'مبلغ', mark: '﷼' },
    { type: 'percent', label: 'درصد', mark: '٪' },
    { type: 'rating', label: 'امتیاز', mark: '★' },
    { type: 'scale', label: 'مقیاس', mark: '↔' },
    { type: 'nps', label: 'NPS', mark: '۱۰' },
    { type: 'yes_no', label: 'بله / خیر', mark: '؟' },
    { type: 'single_choice', label: 'تک‌انتخابی', mark: '◉' },
    { type: 'multi_choice', label: 'چندانتخابی', mark: '☑' },
    { type: 'dropdown', label: 'فهرست کشویی', mark: '⌄' },
    { type: 'multi_dropdown', label: 'کشویی چندتایی', mark: '≣' },
    { type: 'matrix', label: 'ماتریس', mark: '▦' },
    { type: 'persian_date', label: 'تاریخ شمسی', mark: 'ش' },
    { type: 'time', label: 'زمان', mark: '◷' },
    { type: 'datetime', label: 'تاریخ و زمان', mark: 'ش‌ز' },
    { type: 'date_range', label: 'بازهٔ تاریخ', mark: '⇥' },
    { type: 'file', label: 'بارگذاری فایل', mark: '↑' },
    { type: 'signature', label: 'امضا', mark: '✎' },
    { type: 'consent', label: 'رضایت‌نامه', mark: '✓' },
    { type: 'hidden', label: 'مقدار مخفی', mark: '◌' },
    { type: 'calculated', label: 'مقدار محاسباتی', mark: 'ƒ' },
    { type: 'heading', label: 'عنوان نمایشی', mark: 'H' },
    { type: 'description', label: 'متن توضیحی', mark: '≡' },
    { type: 'divider', label: 'جداکننده', mark: '—' },
    { type: 'image', label: 'تصویر', mark: '▧' },
    { type: 'spacer', label: 'فاصله', mark: '↕' }
  ];

  let project: Project | null = null;
  let schema: FormSchema | null = null;
  let revision = 0;
  let selectedPageID = '';
  let selectedFieldID = '';
  let selectedPage: FormPage | undefined;
  let selectedField: FormField | undefined;
  let loading = true;
  let loadError = '';
  let saveStatus: SaveStatus = 'saved';
  let saveMessage = '';
  let saveDetails: Array<{ path: string; message: string }> = [];
  let conflict: { current_revision: number; updated_by: string; updated_at: string } | null = null;
  let saveTimer: ReturnType<typeof setTimeout> | undefined;
  let saveInFlight = false;
  let saveQueued = false;
  let savedAt = '';
  let deleteFieldTarget: FormField | null = null;
  let deletePageTarget: FormPage | null = null;
  let previewMode: 'desktop' | 'mobile' = 'desktop';

  $: canEdit = user.role === 'admin' || user.role === 'designer';
  $: selectedPage = schema?.pages.find((page) => page.id === selectedPageID);
  $: selectedField = selectedPage?.fields.find((field) => field.id === selectedFieldID);

  onMount(() => {
    load();
    const warnBeforeLeave = (event: BeforeUnloadEvent) => {
      if (saveStatus === 'dirty' || saveStatus === 'saving' || saveStatus === 'error' || saveStatus === 'conflict') {
        event.preventDefault();
      }
    };
    window.addEventListener('beforeunload', warnBeforeLeave);
    return () => window.removeEventListener('beforeunload', warnBeforeLeave);
  });

  onDestroy(() => {
    if (saveTimer) clearTimeout(saveTimer);
  });

  async function load() {
    loading = true;
    loadError = '';
    if (saveTimer) clearTimeout(saveTimer);
    try {
      const [loadedProject, loadedDraft] = await Promise.all([api.project(projectID), api.draft(projectID)]);
      project = loadedProject;
      applyDraft(loadedDraft);
    } catch (caught) {
      loadError = messageOf(caught);
    } finally {
      loading = false;
    }
  }

  function applyDraft(loaded: Draft) {
    schema = structuredClone(loaded.schema);
    schema.flow ||= { start_page_id: schema.pages[0]?.id ?? '', edges: [] };
    schema.theme ||= { preset: 'minimal', alignment: 'start' };
    schema.public_settings ||= { minimum_fill_seconds: 2 };
    schema.editor_metadata ||= {};
    revision = loaded.revision;
    selectedPageID = schema.pages[0]?.id ?? '';
    selectedFieldID = '';
    saveStatus = 'saved';
    saveMessage = '';
    saveDetails = [];
    conflict = null;
    savedAt = loaded.updated_at;
  }

  function touch() {
    if (!schema || !canEdit || saveStatus === 'conflict') return;
    schema = { ...schema };
    saveStatus = 'dirty';
    saveMessage = '';
    saveDetails = [];
    if (saveTimer) clearTimeout(saveTimer);
    saveTimer = setTimeout(save, 900);
  }

  async function save() {
    if (!schema || !canEdit || saveStatus === 'conflict') return;
    if (saveInFlight) {
      saveQueued = true;
      return;
    }
    if (saveTimer) clearTimeout(saveTimer);
    saveInFlight = true;
    saveStatus = 'saving';
    const payload = structuredClone(schema);
    const serialized = JSON.stringify(payload);
    try {
      const saved = await api.saveDraft(projectID, revision, payload);
      revision = saved.revision;
      savedAt = saved.updated_at;
      if (schema && JSON.stringify(schema) === serialized) {
        saveStatus = 'saved';
      } else {
        saveStatus = 'dirty';
        saveQueued = true;
      }
    } catch (caught) {
      if (caught instanceof APIError && caught.code === 'draft_conflict') {
        conflict = caught.body.error?.conflict ?? null;
        saveStatus = 'conflict';
        saveMessage = caught.message;
      } else {
        saveStatus = 'error';
        saveMessage = messageOf(caught);
        if (caught instanceof APIError) saveDetails = caught.body.error?.details ?? [];
      }
    } finally {
      saveInFlight = false;
      if (saveQueued && saveStatus !== 'conflict' && saveStatus !== 'error') {
        saveQueued = false;
        saveTimer = setTimeout(save, 250);
      }
    }
  }

  function addPage() {
    if (!schema || !canEdit) return;
    const page: FormPage = { id: crypto.randomUUID(), title: `صفحهٔ ${schema.pages.length + 1}`, fields: [] };
    schema.pages.push(page);
    if (schema.endings[0]) {
      schema.flow.edges.push({
        id: crypto.randomUUID(),
        source_id: page.id,
        target_id: schema.endings[0].id,
        priority: 1000,
        fallback: true,
        actions: []
      });
    }
    selectedPageID = page.id;
    selectedFieldID = '';
    touch();
  }

  function duplicatePage(page: FormPage) {
    if (!schema || !canEdit) return;
    const duplicated = structuredClone(page);
    duplicated.id = crypto.randomUUID();
    duplicated.title = `${page.title} (کپی)`;
    duplicated.fields = duplicated.fields.map(cloneFieldWithNewIDs);
    const index = schema.pages.findIndex((candidate) => candidate.id === page.id);
    schema.pages.splice(index + 1, 0, duplicated);
    if (schema.endings[0]) {
      schema.flow.edges.push({
        id: crypto.randomUUID(),
        source_id: duplicated.id,
        target_id: schema.endings[0].id,
        priority: 1000,
        fallback: true,
        actions: []
      });
    }
    selectedPageID = duplicated.id;
    selectedFieldID = '';
    touch();
  }

  function movePage(index: number, direction: -1 | 1) {
    if (!schema) return;
    const destination = index + direction;
    if (destination < 0 || destination >= schema.pages.length) return;
    [schema.pages[index], schema.pages[destination]] = [schema.pages[destination], schema.pages[index]];
    touch();
  }

  function confirmDeletePage(page: FormPage) {
    if (!schema || schema.pages.length <= 1) return;
    deletePageTarget = page;
  }

  function deletePage() {
    if (!schema || !deletePageTarget) return;
    schema.pages = schema.pages.filter((page) => page.id !== deletePageTarget?.id);
    schema.flow.edges = schema.flow.edges.filter(
      (edge) => edge.source_id !== deletePageTarget?.id && edge.target_id !== deletePageTarget?.id
    );
    if (schema.flow.start_page_id === deletePageTarget.id) schema.flow.start_page_id = schema.pages[0]?.id ?? '';
    selectedPageID = schema.pages[0]?.id ?? '';
    selectedFieldID = '';
    deletePageTarget = null;
    touch();
  }

  function addField(type: FieldType) {
    if (!selectedPage || !canEdit) return;
    const label = catalog.find((item) => item.type === type)?.label ?? 'فیلد';
    const field: FormField = {
      id: crypto.randomUUID(),
      type,
      label: type === 'description' ? '' : label,
      width: 'full'
    };
    if (type === 'description' || type === 'heading')
      field.content = type === 'heading' ? 'عنوان بخش' : 'متن توضیحی را اینجا بنویسید.';
    if (['number', 'money', 'percent'].includes(type)) field.number = {};
    if (['rating', 'scale'].includes(type)) field.scale = { minimum: 1, maximum: 5 };
    if (type === 'nps') field.scale = { minimum: 0, maximum: 10 };
    if (['persian_date', 'datetime', 'date_range'].includes(type)) field.date = {};
    if (['short_text', 'long_text', 'email', 'phone', 'url', 'national_id'].includes(type)) field.text = {};
    if (type === 'national_id') field.text = { national_id: true };
    if (['single_choice', 'multi_choice', 'dropdown', 'multi_dropdown'].includes(type)) {
      field.options = [
        { id: crypto.randomUUID(), label: 'گزینهٔ اول' },
        { id: crypto.randomUUID(), label: 'گزینهٔ دوم' }
      ];
    }
    if (type === 'matrix') {
      field.matrix = {
        rows: [{ id: crypto.randomUUID(), label: 'ردیف اول' }],
        columns: [
          { id: crypto.randomUUID(), label: 'ستون اول' },
          { id: crypto.randomUUID(), label: 'ستون دوم' }
        ]
      };
    }
    if (type === 'file') field.file = { maximum_bytes: 5 * 1024 * 1024, maximum_files: 1, extensions: ['pdf'] };
    if (type === 'consent') field.consent = { agreement_text: 'متن رضایت‌نامه را خوانده‌ام و می‌پذیرم.' };
    if (type === 'image') field.image = { source: 'https://example.com/image.jpg', alt: 'توضیح تصویر' };
    if (isDisplayType(type)) field.label = '';
    selectedPage.fields.push(field);
    selectedFieldID = field.id;
    touch();
  }

  function moveField(index: number, direction: -1 | 1) {
    if (!selectedPage) return;
    const destination = index + direction;
    if (destination < 0 || destination >= selectedPage.fields.length) return;
    [selectedPage.fields[index], selectedPage.fields[destination]] = [
      selectedPage.fields[destination],
      selectedPage.fields[index]
    ];
    touch();
  }

  function duplicateField(field: FormField) {
    if (!selectedPage || !canEdit) return;
    const duplicated = cloneFieldWithNewIDs(field);
    duplicated.label = duplicated.type === 'description' ? duplicated.label : `${duplicated.label} (کپی)`;
    const index = selectedPage.fields.findIndex((candidate) => candidate.id === field.id);
    selectedPage.fields.splice(index + 1, 0, duplicated);
    selectedFieldID = duplicated.id;
    touch();
  }

  function deleteField() {
    if (!selectedPage || !deleteFieldTarget) return;
    selectedPage.fields = selectedPage.fields.filter((field) => field.id !== deleteFieldTarget?.id);
    for (const edge of schema?.flow.edges ?? []) {
      if (edge.condition) removeFieldFromGroup(edge.condition, deleteFieldTarget.id);
      edge.actions = edge.actions?.filter(
        (action) => action.target_field_id !== deleteFieldTarget?.id && action.source_field_id !== deleteFieldTarget?.id
      );
    }
    selectedFieldID = '';
    deleteFieldTarget = null;
    touch();
  }

  function cloneFieldWithNewIDs(field: FormField): FormField {
    const duplicated = structuredClone(field);
    duplicated.id = crypto.randomUUID();
    duplicated.options = duplicated.options?.map((option) => ({ ...option, id: crypto.randomUUID() }));
    if (duplicated.matrix) {
      duplicated.matrix.rows = duplicated.matrix.rows.map((option) => ({ ...option, id: crypto.randomUUID() }));
      duplicated.matrix.columns = duplicated.matrix.columns.map((option) => ({ ...option, id: crypto.randomUUID() }));
    }
    return duplicated;
  }

  function removeFieldFromGroup(group: NonNullable<FormSchema['flow']['edges'][number]['condition']>, fieldID: string) {
    group.conditions = group.conditions?.filter((condition) => condition.field_id !== fieldID);
    for (const child of group.groups ?? []) removeFieldFromGroup(child, fieldID);
  }

  function isDisplayType(type: FieldType): boolean {
    return ['heading', 'description', 'divider', 'image', 'spacer'].includes(type);
  }

  function isChoiceType(type: FieldType): boolean {
    return ['single_choice', 'multi_choice', 'dropdown', 'multi_dropdown'].includes(type);
  }

  function isNumericType(type: FieldType): boolean {
    return ['number', 'money', 'percent'].includes(type);
  }

  function updateField(mutator: (field: FormField) => void) {
    if (!selectedField) return;
    mutator(selectedField);
    touch();
  }

  function updateNumber(key: 'minimum' | 'maximum', raw: string) {
    updateField((field) => {
      field.number ??= {};
      if (raw.trim() === '') delete field.number[key];
      else field.number[key] = Number(raw);
    });
  }

  function updateDate(key: 'minimum' | 'maximum', value: string) {
    updateField((field) => {
      field.date ??= {};
      if (value) field.date[key] = value;
      else delete field.date[key];
    });
  }

  function updateTextLimit(key: 'minimum_length' | 'maximum_length', raw: string) {
    updateField((field) => {
      field.text ??= {};
      if (raw.trim() === '') delete field.text[key];
      else field.text[key] = Number(raw);
    });
  }

  function updateScale(key: 'minimum' | 'maximum', raw: string) {
    updateField((field) => {
      field.scale ??= { minimum: field.type === 'nps' ? 0 : 1, maximum: field.type === 'nps' ? 10 : 5 };
      field.scale[key] = Number(raw);
    });
  }

  function updateFileExtensions(raw: string) {
    updateField((field) => {
      field.file ??= { maximum_bytes: 5 * 1024 * 1024, maximum_files: 1, extensions: ['pdf'] };
      field.file.extensions = raw
        .split(',')
        .map((item) => item.trim().toLowerCase().replace(/^\./, ''))
        .filter(Boolean);
    });
  }

  function addMatrixItem(kind: 'rows' | 'columns') {
    updateField((field) => {
      field.matrix ??= { rows: [], columns: [] };
      field.matrix[kind].push({ id: crypto.randomUUID(), label: kind === 'rows' ? 'ردیف تازه' : 'ستون تازه' });
    });
  }

  function removeMatrixItem(kind: 'rows' | 'columns', optionID: string) {
    updateField((field) => {
      if (!field.matrix) return;
      const minimum = kind === 'rows' ? 1 : 2;
      if (field.matrix[kind].length <= minimum) return;
      field.matrix[kind] = field.matrix[kind].filter((option) => option.id !== optionID);
    });
  }

  function addOption() {
    updateField((field) => {
      field.options ??= [];
      field.options.push({ id: crypto.randomUUID(), label: `گزینهٔ ${field.options.length + 1}` });
    });
  }

  function deleteOption(optionID: string) {
    updateField((field) => {
      if ((field.options?.length ?? 0) <= 2) return;
      field.options = field.options?.filter((option) => option.id !== optionID);
    });
  }

  function saveStatusLabel(): string {
    if (!canEdit) return 'فقط مشاهده';
    return {
      saved: savedAt ? `ذخیره شد · ${formatDateTime(savedAt)}` : 'ذخیره شد',
      dirty: 'تغییرات ذخیره‌نشده',
      saving: 'در حال ذخیره…',
      error: 'ذخیره انجام نشد',
      conflict: 'تداخل ویرایش'
    }[saveStatus];
  }

  function downloadCopy() {
    if (!schema) return;
    const blob = new Blob([JSON.stringify(schema, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `${project?.slug ?? 'form'}-draft-copy.json`;
    link.click();
    URL.revokeObjectURL(url);
  }

  function messageOf(caught: unknown): string {
    return caught instanceof APIError ? caught.message : 'ارتباط با سرور برقرار نشد.';
  }
</script>

{#if loading}
  <div class="builder-loading">
    <div class="loader"></div>
    <span>در حال آماده‌سازی ویرایشگر…</span>
  </div>
{:else if loadError || !schema || !project}
  <main class="startup-error embedded">
    <div class="status-orb error">!</div>
    <h1>فرم باز نشد</h1>
    <p>{loadError}</p>
    <button class="button primary" type="button" onclick={load}>تلاش دوباره</button>
  </main>
{:else}
  <div class="builder-page">
    <header class="builder-header">
      <div class="builder-title">
        <button class="icon-button ghost" type="button" aria-label="بازگشت به پروژه‌ها" onclick={() => onNavigate('/')}
          ><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 18 6-6-6-6" /></svg></button
        >
        <div>
          <span>پروژه / ساخت فرم</span>
          <h1>{project.title}</h1>
        </div>
      </div>
      <div class="save-cluster">
        <span
          class:working={saveStatus === 'saving'}
          class:error-state={saveStatus === 'error' || saveStatus === 'conflict'}
          class="save-status"><i></i>{saveStatusLabel()}</span
        >
        {#if canEdit}<button
            class="button secondary compact-button"
            type="button"
            disabled={saveStatus === 'saving' || saveStatus === 'saved' || saveStatus === 'conflict'}
            onclick={save}>ذخیره اکنون</button
          >{/if}
      </div>
    </header>

    <ProjectTabs {projectID} active="builder" {onNavigate} />

    {#if saveMessage}
      <div class="builder-alert alert error" role="alert">
        <span>{saveMessage}</span>
        {#if saveStatus === 'error'}<button type="button" onclick={save}>تلاش دوباره</button>{/if}
        {#if saveDetails.length}<ul>
            {#each saveDetails.slice(0, 4) as detail}<li>{detail.message}</li>{/each}
          </ul>{/if}
      </div>
    {/if}

    <div class="mobile-builder-note">
      <strong>ویرایش فرم به صفحهٔ بزرگ‌تر نیاز دارد.</strong><span
        >برای مدیریت دقیق فیلدها از رایانه یا تبلت استفاده کنید.</span
      >
    </div>

    <div class="builder-grid">
      <aside class="pages-panel builder-panel">
        <div class="panel-heading">
          <div>
            <span class="step-number">۱</span>
            <h2>صفحه‌ها</h2>
          </div>
          {#if canEdit}<button class="icon-button" type="button" aria-label="افزودن صفحه" onclick={addPage}
              ><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg></button
            >{/if}
        </div>
        <div class="page-list">
          {#each schema.pages as page, index}
            <div class:active={page.id === selectedPageID} class="page-item">
              <button
                class="page-select"
                type="button"
                onclick={() => {
                  selectedPageID = page.id;
                  selectedFieldID = '';
                }}
                ><span>{index + 1}</span>
                <div><strong>{page.title}</strong><small>{page.fields.length} فیلد</small></div></button
              >
              {#if canEdit}
                <div class="reorder-actions">
                  <button
                    type="button"
                    aria-label={`بردن ${page.title} به بالا`}
                    disabled={index === 0}
                    onclick={() => movePage(index, -1)}>↑</button
                  >
                  <button
                    type="button"
                    aria-label={`بردن ${page.title} به پایین`}
                    disabled={index === schema.pages.length - 1}
                    onclick={() => movePage(index, 1)}>↓</button
                  >
                  <button type="button" aria-label={`کپی ${page.title}`} onclick={() => duplicatePage(page)}>⧉</button>
                  <button
                    class="danger-hover"
                    type="button"
                    aria-label={`حذف ${page.title}`}
                    disabled={schema.pages.length === 1}
                    onclick={() => confirmDeletePage(page)}>×</button
                  >
                </div>
              {/if}
            </div>
          {/each}
        </div>
        {#if canEdit}<button class="add-page-button" type="button" onclick={addPage}
            ><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>افزودن صفحه</button
          >{/if}
        <div class="ending-card">
          <span>پایان</span><strong>{schema.endings[0]?.title}</strong><small>ویرایش پایان‌ها در مرحلهٔ مسیرها</small>
        </div>
      </aside>

      <section class="canvas-panel" aria-label="پیش‌نمایش صفحه">
        <div class="canvas-toolbar">
          <div class="viewport-switch">
            <button
              class:active={previewMode === 'desktop'}
              type="button"
              aria-label="نمای دسکتاپ"
              onclick={() => (previewMode = 'desktop')}
              ><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 4h18v12H3zm6 16h6m-3-4v4" /></svg></button
            ><button
              class:active={previewMode === 'mobile'}
              type="button"
              aria-label="نمای موبایل"
              onclick={() => (previewMode = 'mobile')}
              ><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M8 2h8v20H8zM11 18h2" /></svg></button
            >
          </div>
          <span
            >پیش‌نمایش {previewMode === 'mobile' ? 'موبایل' : 'دسکتاپ'} · قالب {schema.theme.preset === 'formal'
              ? 'رسمی'
              : schema.theme.preset === 'warm'
                ? 'گرم'
                : 'مینیمال'}</span
          >
        </div>
        <div
          class:mobile-preview={previewMode === 'mobile'}
          class={`form-canvas builder-theme-${schema.theme.preset}`}
          style={`--builder-accent:${schema.theme.primary_color || '#008d83'}`}
        >
          <div class="canvas-accent"></div>
          {#if selectedPage}
            <div class="canvas-page-heading">
              {#if canEdit}
                <input
                  class="canvas-title-input"
                  aria-label="عنوان صفحه"
                  value={selectedPage.title}
                  oninput={(event) => {
                    selectedPage!.title = event.currentTarget.value;
                    touch();
                  }}
                />
                <textarea
                  class="canvas-description-input"
                  aria-label="توضیح صفحه"
                  placeholder="توضیح کوتاه صفحه (اختیاری)"
                  value={selectedPage.description ?? ''}
                  oninput={(event) => {
                    selectedPage!.description = event.currentTarget.value;
                    touch();
                  }}></textarea>
              {:else}
                <h2>{selectedPage.title}</h2>
                {#if selectedPage.description}<p>{selectedPage.description}</p>{/if}
              {/if}
            </div>
            {#if selectedPage.fields.length === 0}
              <button class="canvas-empty" type="button" disabled={!canEdit} onclick={() => addField('short_text')}
                ><span>+</span><strong>این صفحه هنوز فیلدی ندارد</strong><small
                  >{canEdit
                    ? 'یک فیلد از ستون ابزار انتخاب کنید یا اینجا کلیک کنید.'
                    : 'طراح فرم هنوز فیلدی اضافه نکرده است.'}</small
                ></button
              >
            {:else}
              <div class="field-stack">
                {#each selectedPage.fields as field, index}
                  <div
                    role="button"
                    aria-label={`ویرایش ${field.label || 'متن توضیحی'}`}
                    class:selected={field.id === selectedFieldID}
                    class:half={field.width === 'half'}
                    class="field-preview"
                    onclick={() => (selectedFieldID = field.id)}
                    onkeydown={(event) => {
                      if (event.key === 'Enter' || event.key === ' ') selectedFieldID = field.id;
                    }}
                    tabindex="0"
                  >
                    {#if canEdit}
                      <div class="field-actions">
                        <button
                          type="button"
                          aria-label="بالا بردن فیلد"
                          disabled={index === 0}
                          onclick={(event) => {
                            event.stopPropagation();
                            moveField(index, -1);
                          }}>↑</button
                        >
                        <button
                          type="button"
                          aria-label="پایین بردن فیلد"
                          disabled={index === selectedPage!.fields.length - 1}
                          onclick={(event) => {
                            event.stopPropagation();
                            moveField(index, 1);
                          }}>↓</button
                        >
                        <button
                          type="button"
                          aria-label="کپی فیلد"
                          onclick={(event) => {
                            event.stopPropagation();
                            duplicateField(field);
                          }}>⧉</button
                        >
                        <button
                          class="danger-hover"
                          type="button"
                          aria-label="حذف فیلد"
                          onclick={(event) => {
                            event.stopPropagation();
                            deleteFieldTarget = field;
                          }}>×</button
                        >
                      </div>
                    {/if}
                    {#if field.type === 'heading'}
                      <h3 class="content-preview-heading">{field.content}</h3>
                    {:else if field.type === 'description'}
                      <p class="description-block">{field.content}</p>
                    {:else if field.type === 'divider'}
                      <hr class="content-preview-divider" />
                    {:else if field.type === 'spacer'}
                      <div class="content-preview-spacer"><span>فاصلهٔ عمودی</span></div>
                    {:else if field.type === 'image'}
                      <div class="image-preview">
                        <span>▧</span><strong>{field.image?.alt || 'تصویر'}</strong><small dir="ltr"
                          >{field.image?.source}</small
                        >
                      </div>
                    {:else if field.type === 'hidden' || field.type === 'calculated'}
                      <div class="computed-preview">
                        <span>{field.type === 'hidden' ? 'مقدار مخفی' : 'مقدار محاسباتی'}</span><strong
                          >{field.label}</strong
                        >
                      </div>
                    {:else}
                      <div class="preview-label">
                        {field.label}{#if field.required}<b aria-label="الزامی">*</b>{/if}
                      </div>
                      {#if field.help_text}<small class="field-help">{field.help_text}</small>{/if}
                      {#if ['short_text', 'email', 'phone', 'url', 'national_id'].includes(field.type)}<input
                          disabled
                          aria-hidden="true"
                          placeholder={field.placeholder || 'پاسخ کوتاه'}
                        />{/if}
                      {#if field.type === 'long_text'}<textarea
                          disabled
                          aria-hidden="true"
                          placeholder={field.placeholder || 'پاسخ خود را بنویسید'}></textarea>{/if}
                      {#if isNumericType(field.type)}<input
                          disabled
                          aria-hidden="true"
                          inputmode="decimal"
                          placeholder={field.placeholder || '۰'}
                        />{/if}
                      {#if field.type === 'persian_date'}<DatePicker
                          id={`preview-${field.id}`}
                          value=""
                          disabled
                        />{/if}
                      {#if field.type === 'time' || field.type === 'datetime' || field.type === 'date_range'}<input
                          disabled
                          aria-hidden="true"
                          placeholder={field.type === 'time'
                            ? '۱۴:۳۰'
                            : field.type === 'date_range'
                              ? '۱۴۰۵/۰۶/۱۵ تا ۱۴۰۵/۰۶/۲۰'
                              : '۱۴۰۵/۰۶/۱۵ ۱۴:۳۰'}
                        />{/if}
                      {#if field.type === 'full_name'}<div class="split-preview">
                          <input disabled aria-hidden="true" placeholder="نام" /><input
                            disabled
                            aria-hidden="true"
                            placeholder="نام خانوادگی"
                          />
                        </div>{/if}
                      {#if field.type === 'address'}<textarea
                          disabled
                          aria-hidden="true"
                          placeholder="استان، شهر و نشانی"></textarea>{/if}
                      {#if isChoiceType(field.type)}
                        <div class="choice-preview">
                          {#each field.options ?? [] as option}<span
                              ><i class:checkbox={field.type === 'multi_choice' || field.type === 'multi_dropdown'}
                              ></i>{option.label}</span
                            >{/each}
                        </div>
                      {/if}
                      {#if field.type === 'yes_no'}<div class="choice-preview inline">
                          <span><i></i>بله</span><span><i></i>خیر</span>
                        </div>{/if}
                      {#if field.type === 'rating' || field.type === 'scale' || field.type === 'nps'}<div
                          class="scale-preview"
                        >
                          {#each Array(Math.min(11, (field.scale?.maximum ?? 5) - (field.scale?.minimum ?? 1) + 1)) as _, scaleIndex}<span
                              >{(field.scale?.minimum ?? 1) + scaleIndex}</span
                            >{/each}
                        </div>{/if}
                      {#if field.type === 'matrix'}<div class="matrix-preview">
                          {#each field.matrix?.rows ?? [] as row}<span>{row.label}</span>{/each}<small
                            >{field.matrix?.columns.length ?? 0} ستون پاسخ</small
                          >
                        </div>{/if}
                      {#if field.type === 'file'}<div class="upload-preview">
                          ↑ انتخاب فایل <small>{field.file?.extensions.join('، ')}</small>
                        </div>{/if}
                      {#if field.type === 'signature'}<div class="signature-preview">محل امضا</div>{/if}
                      {#if field.type === 'consent'}<div class="choice-preview">
                          <span><i class="checkbox"></i>{field.consent?.agreement_text}</span>
                        </div>{/if}
                    {/if}
                  </div>
                {/each}
              </div>
            {/if}
            <div class="preview-footer">
              <button class="button primary" type="button" disabled>بعدی</button><span
                >صفحهٔ {schema.pages.findIndex((page) => page.id === selectedPageID) + 1} از {schema.pages.length}</span
              >
            </div>
          {/if}
        </div>
      </section>

      <aside class="inspector-panel builder-panel">
        <div class="panel-heading">
          <div>
            <span class="step-number">۲</span>
            <h2>{selectedField ? 'تنظیمات فیلد' : 'افزودن فیلد'}</h2>
          </div>
          {#if selectedField}<button class="text-button" type="button" onclick={() => (selectedFieldID = '')}
              >بستن</button
            >{/if}
        </div>
        {#if selectedField}
          <div class="inspector-scroll">
            <div class="selected-type">
              <span>{catalog.find((item) => item.type === selectedField?.type)?.mark}</span>
              <div>
                <strong>{catalog.find((item) => item.type === selectedField?.type)?.label}</strong><small
                  >شناسه پایدار: {selectedField.id.slice(0, 8)}</small
                >
              </div>
            </div>
            {#if selectedField.type === 'description' || selectedField.type === 'heading'}
              <div class="field-group">
                <label for="field-content">{selectedField.type === 'heading' ? 'عنوان نمایشی' : 'متن'}</label><textarea
                  id="field-content"
                  rows="6"
                  value={selectedField.content ?? ''}
                  disabled={!canEdit}
                  oninput={(event) => updateField((field) => (field.content = event.currentTarget.value))}></textarea>
              </div>
            {:else if selectedField.type === 'image'}
              <div class="field-group">
                <label for="image-source">نشانی HTTPS تصویر</label><input
                  id="image-source"
                  dir="ltr"
                  value={selectedField.image?.source ?? ''}
                  disabled={!canEdit}
                  oninput={(event) =>
                    updateField((field) => {
                      field.image ??= { source: '', alt: '' };
                      field.image.source = event.currentTarget.value;
                    })}
                />
              </div>
              <div class="field-group">
                <label for="image-alt">متن جایگزین</label><input
                  id="image-alt"
                  value={selectedField.image?.alt ?? ''}
                  disabled={!canEdit}
                  oninput={(event) =>
                    updateField((field) => {
                      field.image ??= { source: '', alt: '' };
                      field.image.alt = event.currentTarget.value;
                    })}
                />
              </div>
            {:else if selectedField.type === 'divider' || selectedField.type === 'spacer'}
              <div class="selected-type-note">این جزء محتوای ورودی ندارد و فقط چیدمان صفحه را تنظیم می‌کند.</div>
            {:else}
              <div class="field-group">
                <label for="field-label">عنوان فیلد</label><textarea
                  id="field-label"
                  rows="2"
                  value={selectedField.label}
                  disabled={!canEdit}
                  oninput={(event) => updateField((field) => (field.label = event.currentTarget.value))}></textarea>
              </div>
              <div class="field-group">
                <label for="field-help">متن راهنما</label><input
                  id="field-help"
                  value={selectedField.help_text ?? ''}
                  disabled={!canEdit}
                  oninput={(event) => updateField((field) => (field.help_text = event.currentTarget.value))}
                />
              </div>
              <div class="field-group">
                <label for="field-placeholder">متن جای‌نگهدار</label><input
                  id="field-placeholder"
                  value={selectedField.placeholder ?? ''}
                  disabled={!canEdit}
                  oninput={(event) => updateField((field) => (field.placeholder = event.currentTarget.value))}
                />
              </div>
              <label class="check-row"
                ><input
                  type="checkbox"
                  checked={selectedField.required ?? false}
                  disabled={!canEdit}
                  onchange={(event) => updateField((field) => (field.required = event.currentTarget.checked))}
                /><span><strong>پاسخ الزامی است</strong><small>بدون پاسخ نمی‌توان از این صفحه عبور کرد.</small></span
                ></label
              >
            {/if}
            <div class="field-group">
              <label for="field-width">عرض در صفحه</label><select
                id="field-width"
                value={selectedField.width ?? 'full'}
                disabled={!canEdit}
                onchange={(event) =>
                  updateField((field) => (field.width = event.currentTarget.value as 'full' | 'half'))}
                ><option value="full">کامل</option><option value="half">نیم‌عرض</option></select
              >
            </div>

            {#if selectedField.text && ['short_text', 'long_text', 'email', 'phone', 'url', 'national_id'].includes(selectedField.type)}
              <div class="inspector-section">
                <h3>اعتبارسنجی متن</h3>
                <div class="inline-fields">
                  <div class="field-group">
                    <label for="text-min">کمینهٔ نویسه</label><input
                      id="text-min"
                      type="number"
                      min="0"
                      value={selectedField.text.minimum_length ?? ''}
                      disabled={!canEdit}
                      oninput={(event) => updateTextLimit('minimum_length', event.currentTarget.value)}
                    />
                  </div>
                  <div class="field-group">
                    <label for="text-max">بیشینهٔ نویسه</label><input
                      id="text-max"
                      type="number"
                      min="0"
                      value={selectedField.text.maximum_length ?? ''}
                      disabled={!canEdit}
                      oninput={(event) => updateTextLimit('maximum_length', event.currentTarget.value)}
                    />
                  </div>
                </div>
                <div class="field-group">
                  <label for="text-pattern">الگوی اختیاری (Regular expression)</label><input
                    id="text-pattern"
                    dir="ltr"
                    value={selectedField.text.pattern ?? ''}
                    disabled={!canEdit}
                    oninput={(event) =>
                      updateField((field) => {
                        field.text ??= {};
                        field.text.pattern = event.currentTarget.value;
                      })}
                  />
                </div>
              </div>
            {/if}

            {#if isNumericType(selectedField.type)}
              <div class="inspector-section">
                <h3>محدودهٔ عدد</h3>
                <div class="inline-fields">
                  <div class="field-group">
                    <label for="number-min">کمینه</label><input
                      id="number-min"
                      type="number"
                      value={selectedField.number?.minimum ?? ''}
                      disabled={!canEdit}
                      oninput={(event) => updateNumber('minimum', event.currentTarget.value)}
                    />
                  </div>
                  <div class="field-group">
                    <label for="number-max">بیشینه</label><input
                      id="number-max"
                      type="number"
                      value={selectedField.number?.maximum ?? ''}
                      disabled={!canEdit}
                      oninput={(event) => updateNumber('maximum', event.currentTarget.value)}
                    />
                  </div>
                </div>
                <label class="check-row"
                  ><input
                    type="checkbox"
                    checked={selectedField.number?.integer ?? false}
                    disabled={!canEdit}
                    onchange={(event) =>
                      updateField((field) => {
                        field.number ??= {};
                        field.number.integer = event.currentTarget.checked;
                      })}
                  /><span><strong>فقط عدد صحیح</strong></span></label
                >
              </div>
            {/if}

            {#if selectedField.type === 'persian_date' || selectedField.type === 'datetime' || selectedField.type === 'date_range'}
              <div class="inspector-section">
                <h3>محدودهٔ تاریخ</h3>
                <DatePicker
                  id="date-min"
                  label="از تاریخ"
                  value={selectedField.date?.minimum ?? ''}
                  disabled={!canEdit}
                  onValueChange={(value) => updateDate('minimum', value)}
                /><DatePicker
                  id="date-max"
                  label="تا تاریخ"
                  value={selectedField.date?.maximum ?? ''}
                  disabled={!canEdit}
                  onValueChange={(value) => updateDate('maximum', value)}
                /><small class="storage-note">در رابط شمسی؛ در API و پایگاه داده ISO استاندارد.</small>
              </div>
            {/if}

            {#if isChoiceType(selectedField.type)}
              <div class="inspector-section">
                <div class="section-heading compact">
                  <h3>گزینه‌ها</h3>
                  {#if canEdit}<button class="text-button" type="button" onclick={addOption}>+ افزودن</button>{/if}
                </div>
                <div class="option-list">
                  {#each selectedField.options ?? [] as option, index}<div>
                      <span>⋮⋮</span><input
                        aria-label={`گزینهٔ ${index + 1}`}
                        value={option.label}
                        disabled={!canEdit}
                        oninput={(event) =>
                          updateField((field) => {
                            const target = field.options?.find((candidate) => candidate.id === option.id);
                            if (target) target.label = event.currentTarget.value;
                          })}
                      />{#if canEdit}<button
                          type="button"
                          aria-label="حذف گزینه"
                          disabled={(selectedField?.options?.length ?? 0) <= 2}
                          onclick={() => deleteOption(option.id)}>×</button
                        >{/if}
                    </div>{/each}
                </div>
              </div>
            {/if}

            {#if selectedField.type === 'rating' || selectedField.type === 'scale' || selectedField.type === 'nps'}
              <div class="inspector-section">
                <h3>محدودهٔ سنجش</h3>
                <div class="inline-fields">
                  <div class="field-group">
                    <label for="scale-min">کمینه</label><input
                      id="scale-min"
                      type="number"
                      value={selectedField.scale?.minimum ?? (selectedField.type === 'nps' ? 0 : 1)}
                      disabled={!canEdit}
                      oninput={(event) => updateScale('minimum', event.currentTarget.value)}
                    />
                  </div>
                  <div class="field-group">
                    <label for="scale-max">بیشینه</label><input
                      id="scale-max"
                      type="number"
                      value={selectedField.scale?.maximum ?? (selectedField.type === 'nps' ? 10 : 5)}
                      disabled={!canEdit}
                      oninput={(event) => updateScale('maximum', event.currentTarget.value)}
                    />
                  </div>
                </div>
                <div class="field-group">
                  <label for="scale-low-label">برچسب سمت کمینه</label><input
                    id="scale-low-label"
                    value={selectedField.scale?.low_label ?? ''}
                    disabled={!canEdit}
                    oninput={(event) =>
                      updateField((field) => {
                        field.scale ??= { minimum: 1, maximum: 5 };
                        field.scale.low_label = event.currentTarget.value;
                      })}
                  />
                </div>
                <div class="field-group">
                  <label for="scale-high-label">برچسب سمت بیشینه</label><input
                    id="scale-high-label"
                    value={selectedField.scale?.high_label ?? ''}
                    disabled={!canEdit}
                    oninput={(event) =>
                      updateField((field) => {
                        field.scale ??= { minimum: 1, maximum: 5 };
                        field.scale.high_label = event.currentTarget.value;
                      })}
                  />
                </div>
              </div>
            {/if}

            {#if selectedField.type === 'file'}
              <div class="inspector-section">
                <h3>محدودیت فایل</h3>
                <div class="inline-fields">
                  <div class="field-group">
                    <label for="file-size">حداکثر حجم هر فایل (MB)</label><input
                      id="file-size"
                      type="number"
                      min="0.001"
                      max="100"
                      step="0.25"
                      value={(selectedField.file?.maximum_bytes ?? 5242880) / 1048576}
                      disabled={!canEdit}
                      oninput={(event) =>
                        updateField((field) => {
                          field.file ??= { maximum_bytes: 5242880, maximum_files: 1, extensions: ['pdf'] };
                          field.file.maximum_bytes = Math.round(Number(event.currentTarget.value) * 1048576);
                        })}
                    />
                  </div>
                  <div class="field-group">
                    <label for="file-count">حداکثر تعداد</label><input
                      id="file-count"
                      type="number"
                      min="1"
                      max="20"
                      value={selectedField.file?.maximum_files ?? 1}
                      disabled={!canEdit}
                      oninput={(event) =>
                        updateField((field) => {
                          field.file ??= { maximum_bytes: 5242880, maximum_files: 1, extensions: ['pdf'] };
                          field.file.maximum_files = Number(event.currentTarget.value);
                        })}
                    />
                  </div>
                </div>
                <div class="field-group">
                  <label for="file-extensions">پسوندهای مجاز (با ویرگول)</label><input
                    id="file-extensions"
                    dir="ltr"
                    value={selectedField.file?.extensions.join(', ') ?? 'pdf'}
                    disabled={!canEdit}
                    oninput={(event) => updateFileExtensions(event.currentTarget.value)}
                  /><small>pdf, jpg, jpeg, png, webp, txt, csv, docx, xlsx, zip</small>
                </div>
              </div>
            {/if}

            {#if selectedField.type === 'consent'}
              <div class="inspector-section">
                <h3>متن رضایت</h3>
                <div class="field-group">
                  <label for="consent-text">متن توافق</label><textarea
                    id="consent-text"
                    rows="4"
                    value={selectedField.consent?.agreement_text ?? ''}
                    disabled={!canEdit}
                    oninput={(event) =>
                      updateField((field) => {
                        field.consent ??= { agreement_text: '' };
                        field.consent.agreement_text = event.currentTarget.value;
                      })}></textarea>
                </div>
                <div class="field-group">
                  <label for="consent-link-label">عنوان پیوند</label><input
                    id="consent-link-label"
                    value={selectedField.consent?.link_label ?? ''}
                    disabled={!canEdit}
                    oninput={(event) =>
                      updateField((field) => {
                        field.consent ??= { agreement_text: '' };
                        field.consent.link_label = event.currentTarget.value;
                      })}
                  />
                </div>
                <div class="field-group">
                  <label for="consent-link-url">نشانی HTTPS پیوند</label><input
                    id="consent-link-url"
                    dir="ltr"
                    value={selectedField.consent?.link_url ?? ''}
                    disabled={!canEdit}
                    oninput={(event) =>
                      updateField((field) => {
                        field.consent ??= { agreement_text: '' };
                        field.consent.link_url = event.currentTarget.value;
                      })}
                  />
                </div>
              </div>
            {/if}

            {#if selectedField.type === 'matrix' && selectedField.matrix}
              <div class="inspector-section">
                <div class="section-heading compact">
                  <h3>سطرهای ماتریس</h3>
                  <button class="text-button" type="button" disabled={!canEdit} onclick={() => addMatrixItem('rows')}
                    >+ سطر</button
                  >
                </div>
                <div class="option-list">
                  {#each selectedField.matrix.rows as row, index}<div>
                      <span>⋮⋮</span><input
                        aria-label={`سطر ${index + 1}`}
                        bind:value={row.label}
                        disabled={!canEdit}
                        oninput={touch}
                      /><button
                        type="button"
                        aria-label="حذف سطر"
                        disabled={!canEdit || selectedField.matrix.rows.length <= 1}
                        onclick={() => removeMatrixItem('rows', row.id)}>×</button
                      >
                    </div>{/each}
                </div>
                <div class="section-heading compact">
                  <h3>ستون‌های ماتریس</h3>
                  <button class="text-button" type="button" disabled={!canEdit} onclick={() => addMatrixItem('columns')}
                    >+ ستون</button
                  >
                </div>
                <div class="option-list">
                  {#each selectedField.matrix.columns as column, index}<div>
                      <span>⋮⋮</span><input
                        aria-label={`ستون ${index + 1}`}
                        bind:value={column.label}
                        disabled={!canEdit}
                        oninput={touch}
                      /><button
                        type="button"
                        aria-label="حذف ستون"
                        disabled={!canEdit || selectedField.matrix.columns.length <= 2}
                        onclick={() => removeMatrixItem('columns', column.id)}>×</button
                      >
                    </div>{/each}
                </div>
                <label class="check-row"
                  ><input
                    type="checkbox"
                    bind:checked={selectedField.matrix.multiple}
                    disabled={!canEdit}
                    onchange={touch}
                  /><span><strong>چند انتخاب در هر سطر</strong></span></label
                >
              </div>
            {/if}

            {#if selectedField.type === 'hidden' || selectedField.type === 'calculated'}
              <div class="inspector-section">
                <h3>مقدار اولیه</h3>
                <div class="field-group">
                  <label for="default-value">مقدار</label><input
                    id="default-value"
                    value={String(selectedField.default_value ?? '')}
                    disabled={!canEdit}
                    oninput={(event) => updateField((field) => (field.default_value = event.currentTarget.value))}
                  />
                </div>
                <small class="storage-note">مسیرها می‌توانند این مقدار را با عمل مقداردهی تغییر دهند.</small>
              </div>
            {/if}

            {#if canEdit}<button
                class="button danger-outline wide"
                type="button"
                onclick={() => (deleteFieldTarget = selectedField ?? null)}>حذف این فیلد</button
              >{/if}
          </div>
        {:else}
          <div class="palette-copy"><p>نوع فیلد را انتخاب کنید تا به «{selectedPage?.title}» افزوده شود.</p></div>
          <div class="field-palette">
            {#each catalog as item}<button type="button" disabled={!canEdit} onclick={() => addField(item.type)}
                ><span>{item.mark}</span><strong>{item.label}</strong><small>افزودن به صفحه</small></button
              >{/each}
          </div>
        {/if}
      </aside>
    </div>
  </div>
{/if}

{#if conflict}
  <Modal
    title="تداخل در ویرایش"
    description="این پیش‌نویس پس از بازشدن صفحه توسط فرد دیگری ذخیره شده است."
    dismissible={false}
    onClose={() => undefined}
  >
    <div class="conflict-box">
      <p>نسخهٔ فعلی سرور: <strong>{conflict.current_revision}</strong></p>
      <p>زمان تغییر: <strong>{formatDateTime(conflict.updated_at)}</strong></p>
    </div>
    <p class="modal-note">ابتدا می‌توانید یک کپی JSON از تغییرات خود بگیرید؛ سپس نسخهٔ تازهٔ سرور را بارگذاری کنید.</p>
    <div class="modal-actions">
      <button class="button secondary" type="button" onclick={downloadCopy}>دریافت نسخهٔ من</button><button
        class="button primary"
        type="button"
        onclick={load}>بارگذاری نسخهٔ سرور</button
      >
    </div>
  </Modal>
{/if}

{#if deleteFieldTarget}
  <Modal
    title="حذف فیلد"
    description={`فیلد «${deleteFieldTarget.label || 'متن توضیحی'}» از پیش‌نویس حذف می‌شود.`}
    onClose={() => (deleteFieldTarget = null)}
    ><div class="modal-actions">
      <button class="button secondary" type="button" onclick={() => (deleteFieldTarget = null)}>انصراف</button><button
        class="button danger"
        type="button"
        onclick={deleteField}>حذف فیلد</button
      >
    </div></Modal
  >
{/if}

{#if deletePageTarget}
  <Modal
    title="حذف صفحه"
    description={`صفحهٔ «${deletePageTarget.title}» و همهٔ فیلدهای داخل آن حذف می‌شود.`}
    onClose={() => (deletePageTarget = null)}
    ><div class="modal-actions">
      <button class="button secondary" type="button" onclick={() => (deletePageTarget = null)}>انصراف</button><button
        class="button danger"
        type="button"
        onclick={deletePage}>حذف صفحه</button
      >
    </div></Modal
  >
{/if}
