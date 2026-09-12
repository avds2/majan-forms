<script lang="ts">
  import { onMount } from 'svelte';
  import { api, APIError } from '../api';
  import { evaluateFlow } from '../flow-evaluator';
  import { formatDateTime, normalizeDigits, toPersianDigits } from '../persian-date';
  import type {
    AssignmentAction,
    Draft,
    FlowCondition,
    FlowEdge,
    FormField,
    FormSchema,
    Project,
    User
  } from '../types';
  import FlowCanvas from './FlowCanvas.svelte';
  import ProjectTabs from './ProjectTabs.svelte';

  export let user: User;
  export let projectID: string;
  export let onNavigate: (path: string) => void;

  let project: Project | null = null;
  let schema: FormSchema | null = null;
  let revision = 0;
  let updatedAt = '';
  let loading = true;
  let error = '';
  let details: Array<{ path: string; message: string }> = [];
  let saving = false;
  let dirty = false;
  let selectedEdgeID = '';
  let testAnswers: Record<string, string> = {};
  let testedPath: string[] = [];
  let testError = '';

  $: canEdit = user.role === 'admin' || user.role === 'designer';
  $: selectedEdge = schema?.flow.edges.find((edge) => edge.id === selectedEdgeID);
  $: allConditionFields = schema ? schema.pages.flatMap((page) => page.fields).filter(isConditionField) : [];
  $: eligibleFields = selectedEdge ? conditionFieldsForSource(selectedEdge.source_id) : allConditionFields;
  $: warnings = schema ? graphWarnings(schema) : [];

  onMount(load);

  async function load() {
    loading = true;
    error = '';
    try {
      const [loadedProject, loadedDraft] = await Promise.all([api.project(projectID), api.draft(projectID)]);
      project = loadedProject;
      applyDraft(loadedDraft);
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      loading = false;
    }
  }

  function applyDraft(draft: Draft) {
    schema = structuredClone(draft.schema);
    ensureStageTwoSchema(schema);
    revision = draft.revision;
    updatedAt = draft.updated_at;
    selectedEdgeID = schema.flow.edges[0]?.id ?? '';
    dirty = false;
    details = [];
  }

  function touch() {
    if (!schema || !canEdit) return;
    schema = { ...schema };
    dirty = true;
    error = '';
    details = [];
    testedPath = [];
  }

  async function save(): Promise<boolean> {
    if (!schema || !canEdit) return false;
    saving = true;
    error = '';
    details = [];
    try {
      const saved = await api.saveDraft(projectID, revision, structuredClone(schema));
      applyDraft(saved);
      return true;
    } catch (caught) {
      error = messageOf(caught);
      if (caught instanceof APIError) details = caught.body.error?.details ?? [];
      return false;
    } finally {
      saving = false;
    }
  }

  async function validateOnServer() {
    if (dirty && !(await save())) return;
    error = '';
    details = [];
    try {
      await api.validatePublish(projectID);
      error = 'ساختار مسیرها برای انتشار معتبر است.';
    } catch (caught) {
      error = messageOf(caught);
      if (caught instanceof APIError) details = caught.body.error?.details ?? [];
    }
  }

  function addRoute() {
    if (!schema || !canEdit) return;
    const source = schema.flow.start_page_id || schema.pages[0]?.id;
    const target = schema.endings[0]?.id || schema.pages.find((page) => page.id !== source)?.id;
    if (!source || !target) return;
    const sourceEdges = schema.flow.edges.filter((edge) => edge.source_id === source);
    const edge: FlowEdge = {
      id: crypto.randomUUID(),
      source_id: source,
      target_id: target,
      priority: Math.max(0, ...sourceEdges.map((item) => item.priority)) + 10,
      actions: []
    };
    if (sourceEdges.some((item) => item.fallback)) edge.condition = defaultCondition(source);
    else edge.fallback = true;
    schema.flow.edges.push(edge);
    selectedEdgeID = edge.id;
    touch();
  }

  function deleteRoute(edge: FlowEdge) {
    if (!schema || !canEdit) return;
    schema.flow.edges = schema.flow.edges.filter((candidate) => candidate.id !== edge.id);
    selectedEdgeID = schema.flow.edges[0]?.id ?? '';
    touch();
  }

  function setFallback(edge: FlowEdge, fallback: boolean) {
    if (!schema) return;
    if (fallback) {
      for (const candidate of schema.flow.edges) {
        if (candidate.source_id === edge.source_id && candidate.id !== edge.id && candidate.fallback) {
          candidate.fallback = false;
          candidate.condition = defaultCondition(candidate.source_id);
        }
      }
      edge.fallback = true;
      delete edge.condition;
    } else {
      edge.fallback = false;
      edge.condition = defaultCondition(edge.source_id);
    }
    touch();
  }

  function defaultCondition(sourceID: string): { match: 'all'; conditions: FlowCondition[] } {
    const field = conditionFieldsForSource(sourceID)[0];
    return {
      match: 'all',
      conditions: field ? [newCondition(field)] : []
    };
  }

  function newCondition(field: FormField): FlowCondition {
    const operator = operatorsFor(field)[0]?.value ?? 'equals';
    const condition: FlowCondition = { field_id: field.id, operator };
    if (operator !== 'empty' && operator !== 'not_empty') condition.value = defaultValue(field, operator);
    return condition;
  }

  function addCondition(edge: FlowEdge) {
    const field = eligibleFields[0];
    if (!field) return;
    edge.fallback = false;
    edge.condition ??= { match: 'all', conditions: [] };
    edge.condition.conditions ??= [];
    edge.condition.conditions.push(newCondition(field));
    touch();
  }

  function removeCondition(edge: FlowEdge, index: number) {
    edge.condition?.conditions?.splice(index, 1);
    touch();
  }

  function updateConditionField(condition: FlowCondition, fieldID: string) {
    const field = eligibleFields.find((candidate) => candidate.id === fieldID);
    if (!field) return;
    Object.assign(condition, newCondition(field));
    if (condition.operator === 'empty' || condition.operator === 'not_empty') delete condition.value;
    touch();
  }

  function updateConditionOperator(condition: FlowCondition, operator: string) {
    condition.operator = operator;
    if (operator === 'empty' || operator === 'not_empty') delete condition.value;
    else {
      const field = eligibleFields.find((candidate) => candidate.id === condition.field_id);
      condition.value = field ? defaultValue(field, operator) : '';
    }
    touch();
  }

  function updateConditionValue(condition: FlowCondition, raw: string) {
    const field = eligibleFields.find((candidate) => candidate.id === condition.field_id);
    condition.value = parseFieldValue(field, raw, condition.operator);
    touch();
  }

  function updateConditionSelections(condition: FlowCondition, select: HTMLSelectElement) {
    condition.value = Array.from(select.selectedOptions, (option) => option.value);
    touch();
  }

  function addAction(edge: FlowEdge) {
    const target = assignableFields(edge)[0];
    if (!target) return;
    edge.actions ??= [];
    edge.actions.push({
      id: crypto.randomUUID(),
      target_field_id: target.id,
      mode: 'replace',
      source: 'constant',
      value: defaultValue(target)
    });
    touch();
  }

  function removeAction(edge: FlowEdge, index: number) {
    edge.actions?.splice(index, 1);
    touch();
  }

  function updateActionValue(action: AssignmentAction, raw: string) {
    const field = assignableFields(selectedEdge).find((candidate) => candidate.id === action.target_field_id);
    action.value = parseFieldValue(field, raw, 'equals');
    touch();
  }

  function updateActionSelections(action: AssignmentAction, select: HTMLSelectElement) {
    action.value = Array.from(select.selectedOptions, (option) => option.value);
    touch();
  }

  function updateActionTarget(action: AssignmentAction, targetID: string) {
    const field = assignableFields(selectedEdge).find((candidate) => candidate.id === targetID);
    if (!field) return;
    action.target_field_id = field.id;
    if (action.source === 'constant') action.value = defaultValue(field);
    else action.source_field_id = compatibleSourceFields(action)[0]?.id;
    touch();
  }

  function updateActionSource(action: AssignmentAction, source: 'constant' | 'copy') {
    action.source = source;
    const target = assignableFields(selectedEdge).find((field) => field.id === action.target_field_id);
    if (source === 'constant') {
      delete action.source_field_id;
      action.value = target ? defaultValue(target) : '';
    } else {
      delete action.value;
      action.source_field_id = compatibleSourceFields(action)[0]?.id;
    }
    touch();
  }

  function addEnding() {
    if (!schema || !canEdit) return;
    schema.endings.push({
      id: crypto.randomUUID(),
      title: `پایان ${schema.endings.length + 1}`,
      message: 'سپاس از شما.'
    });
    touch();
  }

  function removeEnding(endingID: string) {
    if (!schema || schema.endings.length <= 1) return;
    schema.endings = schema.endings.filter((ending) => ending.id !== endingID);
    schema.flow.edges = schema.flow.edges.filter((edge) => edge.target_id !== endingID);
    touch();
  }

  function storePosition(id: string, x: number, y: number) {
    if (!schema || !canEdit) return;
    const metadata = schema.editor_metadata as Record<string, unknown>;
    const positions = (metadata.flow_positions ??= {}) as Record<string, { x: number; y: number }>;
    positions[id] = { x: Math.round(x), y: Math.round(y) };
    touch();
  }

  function runTest() {
    if (!schema) return;
    const answers: Record<string, unknown> = {};
    for (const field of allConditionFields)
      answers[field.id] = parseFieldValue(field, testAnswers[field.id] ?? '', 'equals');
    try {
      const result = evaluateFlow(schema, answers);
      testedPath = [...result.pageIDs, result.endingID];
      testError = '';
    } catch (caught) {
      testError = caught instanceof Error ? caught.message : 'آزمایش مسیر کامل نشد.';
      testedPath = [];
    }
  }

  function parseFieldValue(field: FormField | undefined, raw: string, operator: string): unknown {
    const normalizedRaw = normalizeDigits(raw.trim());
    if (operator === 'between') {
      const values = normalizedRaw.split(',').map((value) => value.trim());
      return isNumeric(field) ? values.map(Number) : values;
    }
    if (operator === 'contains_any' || operator === 'contains_all' || isMulti(field)) {
      return normalizedRaw
        .split(',')
        .map((value) => value.trim())
        .filter(Boolean);
    }
    if (isNumeric(field)) return Number(normalizedRaw || 0);
    if (field?.type === 'yes_no' || field?.type === 'consent') return normalizedRaw === 'true';
    return raw.trim();
  }

  function defaultValue(field: FormField, operator = 'equals'): unknown {
    if (operator === 'between') return isNumeric(field) ? [0, 1] : ['2026-01-01', '2026-12-31'];
    if (operator === 'contains_any' || operator === 'contains_all' || isMulti(field)) {
      const first = fieldOptions(field)[0]?.id;
      return first ? [first] : [];
    }
    if (isNumeric(field)) return 0;
    if (field.type === 'yes_no' || field.type === 'consent') return false;
    if (field.options?.length) return field.options[0].id;
    return '';
  }

  function displayValue(value: unknown): string {
    return Array.isArray(value) ? value.join(', ') : String(value ?? '');
  }

  function nodeLabel(nodeID: string): string {
    return (
      schema?.pages.find((page) => page.id === nodeID)?.title ??
      schema?.endings.find((ending) => ending.id === nodeID)?.title ??
      'مقصد حذف‌شده'
    );
  }

  function operatorsFor(field: FormField): Array<{ value: string; label: string }> {
    const common = [
      { value: 'empty', label: 'خالی است' },
      { value: 'not_empty', label: 'خالی نیست' }
    ];
    if (isNumeric(field))
      return [
        { value: 'equals', label: 'برابر است' },
        { value: 'not_equals', label: 'برابر نیست' },
        { value: 'greater_than', label: 'بزرگ‌تر است' },
        { value: 'less_than', label: 'کوچک‌تر است' },
        { value: 'between', label: 'بین دو مقدار است' },
        ...common
      ];
    if (field.type === 'date_range' || field.type === 'full_name' || field.type === 'address') return common;
    if (field.type === 'persian_date' || field.type === 'datetime' || field.type === 'time')
      return [
        { value: 'equals', label: 'برابر است' },
        { value: 'before', label: 'پیش از' },
        { value: 'after', label: 'پس از' },
        { value: 'between', label: 'بین دو مقدار است' },
        ...common
      ];
    if (isMulti(field) || field.type === 'matrix')
      return [
        { value: 'contains_any', label: 'شامل یکی از' },
        { value: 'contains_all', label: 'شامل همهٔ' },
        ...common
      ];
    if (
      field.type === 'yes_no' ||
      field.type === 'consent' ||
      field.type === 'single_choice' ||
      field.type === 'dropdown'
    )
      return [{ value: 'equals', label: 'برابر است' }, { value: 'not_equals', label: 'برابر نیست' }, ...common];
    return [
      { value: 'equals', label: 'برابر است' },
      { value: 'not_equals', label: 'برابر نیست' },
      { value: 'contains', label: 'شامل است' },
      { value: 'not_contains', label: 'شامل نیست' },
      ...common
    ];
  }

  function isNumeric(field?: FormField): boolean {
    return !!field && ['number', 'money', 'percent', 'rating', 'scale', 'nps', 'calculated'].includes(field.type);
  }

  function isMulti(field?: FormField): boolean {
    return !!field && ['multi_choice', 'multi_dropdown'].includes(field.type);
  }

  function fieldOptions(field?: FormField): Array<{ id: string; label: string }> {
    if (!field) return [];
    return field.type === 'matrix' ? (field.matrix?.columns ?? []) : (field.options ?? []);
  }

  function isBoolean(field?: FormField): boolean {
    return field?.type === 'yes_no' || field?.type === 'consent';
  }

  function compatibleSourceFields(action: AssignmentAction): FormField[] {
    const target = assignableFields(selectedEdge).find((field) => field.id === action.target_field_id);
    if (!target) return [];
    return conditionFieldsForSource(selectedEdge?.source_id ?? '').filter(
      (field) => field.id !== target.id && field.type === target.type
    );
  }

  function isConditionField(field: FormField): boolean {
    return !['heading', 'description', 'divider', 'image', 'spacer', 'file', 'signature'].includes(field.type);
  }

  function conditionFieldsForSource(sourceID: string): FormField[] {
    if (!schema) return [];
    const allowedPages = new Set<string>([sourceID]);
    const queue = [sourceID];
    while (queue.length) {
      const node = queue.shift()!;
      for (const edge of schema.flow.edges) {
        if (edge.target_id === node && !allowedPages.has(edge.source_id)) {
          allowedPages.add(edge.source_id);
          queue.push(edge.source_id);
        }
      }
    }
    return schema.pages.flatMap((page) =>
      page.fields.filter(
        (field) =>
          isConditionField(field) && (allowedPages.has(page.id) || ['hidden', 'calculated'].includes(field.type))
      )
    );
  }

  function allAssignableFields(): FormField[] {
    return (
      schema?.pages
        .flatMap((page) => page.fields)
        .filter(
          (field) =>
            ![
              'heading',
              'description',
              'divider',
              'image',
              'spacer',
              'file',
              'signature',
              'full_name',
              'address',
              'date_range',
              'matrix'
            ].includes(field.type)
        ) ?? []
    );
  }

  function assignableFields(edge: FlowEdge | undefined = selectedEdge): FormField[] {
    if (!schema || !edge) return [];
    const sourceIndex = schema.pages.findIndex((page) => page.id === edge.source_id);
    return schema.pages.flatMap((page, pageIndex) =>
      page.fields.filter(
        (field) =>
          allAssignableFields().some((candidate) => candidate.id === field.id) &&
          (['hidden', 'calculated'].includes(field.type) ||
            (pageIndex > sourceIndex && pageCanReach(edge.target_id, page.id)))
      )
    );
  }

  function pageCanReach(from: string, target: string): boolean {
    if (!schema) return false;
    const queue = [from];
    const seen = new Set<string>();
    while (queue.length) {
      const node = queue.shift()!;
      if (node === target) return true;
      if (seen.has(node)) continue;
      seen.add(node);
      for (const edge of schema.flow.edges) if (edge.source_id === node) queue.push(edge.target_id);
    }
    return false;
  }

  function graphWarnings(current: FormSchema): string[] {
    const result: string[] = [];
    const nodeIDs = new Set([...current.pages.map((page) => page.id), ...current.endings.map((ending) => ending.id)]);
    if (!current.pages.some((page) => page.id === current.flow.start_page_id)) result.push('صفحهٔ شروع معتبر نیست.');
    for (const page of current.pages) {
      const outgoing = current.flow.edges.filter((edge) => edge.source_id === page.id);
      if (outgoing.filter((edge) => edge.fallback).length !== 1)
        result.push(`صفحهٔ «${page.title}» باید دقیقاً یک مسیر پیش‌فرض داشته باشد.`);
    }
    if (current.flow.edges.some((edge) => !nodeIDs.has(edge.source_id) || !nodeIDs.has(edge.target_id)))
      result.push('حداقل یک مسیر به صفحه یا پایان حذف‌شده اشاره می‌کند.');
    const reachable = new Set<string>();
    const queue = [current.flow.start_page_id];
    while (queue.length) {
      const node = queue.shift()!;
      if (reachable.has(node)) continue;
      reachable.add(node);
      current.flow.edges.filter((edge) => edge.source_id === node).forEach((edge) => queue.push(edge.target_id));
    }
    const unreachable = [...current.pages, ...current.endings].filter((node) => !reachable.has(node.id));
    if (unreachable.length)
      result.push(`${toPersianDigits(unreachable.length)} صفحه یا پایان از نقطهٔ شروع قابل‌دسترسی نیست.`);
    if (hasCycle(current)) result.push('نمودار دارای چرخه است؛ انتشار تا رفع چرخه ممکن نیست.');
    return result;
  }

  function hasCycle(current: FormSchema): boolean {
    const pages = new Set(current.pages.map((page) => page.id));
    const visiting = new Set<string>();
    const visited = new Set<string>();
    const visit = (node: string): boolean => {
      if (visiting.has(node)) return true;
      if (visited.has(node)) return false;
      visiting.add(node);
      for (const edge of current.flow.edges.filter((candidate) => candidate.source_id === node)) {
        if (pages.has(edge.target_id) && visit(edge.target_id)) return true;
      }
      visiting.delete(node);
      visited.add(node);
      return false;
    };
    return current.pages.some((page) => visit(page.id));
  }

  function ensureStageTwoSchema(current: FormSchema) {
    current.endings ||= [{ id: crypto.randomUUID(), title: 'پایان اصلی', message: 'سپاس از شما.' }];
    current.flow ||= { start_page_id: current.pages[0]?.id ?? '', edges: [] };
    current.theme ||= { preset: 'minimal', alignment: 'start' };
    current.public_settings ||= { minimum_fill_seconds: 2 };
    current.editor_metadata ||= {};
  }

  function messageOf(caught: unknown): string {
    return caught instanceof APIError ? caught.message : 'ارتباط با سرور برقرار نشد.';
  }
</script>

{#if loading}
  <div class="builder-loading">
    <div class="loader"></div>
    <span>در حال آماده‌سازی مسیرها…</span>
  </div>
{:else if !schema || !project}
  <main class="startup-error embedded">
    <div class="status-orb error">!</div>
    <h1>مسیرها باز نشد</h1>
    <p>{error}</p>
    <button class="button primary" type="button" onclick={load}>تلاش دوباره</button>
  </main>
{:else}
  <div class="builder-page routes-page">
    <header class="builder-header">
      <div class="builder-title">
        <button class="icon-button ghost" type="button" aria-label="بازگشت به پروژه‌ها" onclick={() => onNavigate('/')}
          ><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 18 6-6-6-6" /></svg></button
        >
        <div>
          <span>پروژه / مسیرها</span>
          <h1>{project.title}</h1>
        </div>
      </div>
      <div class="save-cluster">
        <span class:working={saving} class="save-status"
          ><i></i>{dirty ? 'تغییرات ذخیره‌نشده' : `ذخیره‌شده · ${formatDateTime(updatedAt)}`}</span
        >{#if canEdit}<button
            class="button secondary compact-button"
            type="button"
            disabled={!dirty || saving}
            onclick={save}>{saving ? 'در حال ذخیره…' : 'ذخیره مسیرها'}</button
          >{/if}
      </div>
    </header>
    <ProjectTabs {projectID} active="routes" {onNavigate} />

    {#if error}<div
        class:success={error.includes('معتبر است')}
        class="builder-alert alert"
        class:error={!error.includes('معتبر است')}
        role="status"
      >
        <span>{error}</span>{#if details.length}<ul>
            {#each details.slice(0, 6) as detail}<li>{detail.message}</li>{/each}
          </ul>{/if}
      </div>{/if}

    <section class="route-command-bar">
      <div class="field-group">
        <label for="start-page">صفحهٔ شروع</label><select
          id="start-page"
          bind:value={schema.flow.start_page_id}
          disabled={!canEdit}
          onchange={touch}
          >{#each schema.pages as page}<option value={page.id}>{page.title}</option>{/each}</select
        >
      </div>
      <button class="button secondary" type="button" disabled={!canEdit} onclick={addRoute}>+ مسیر تازه</button>
      <button class="button secondary" type="button" onclick={validateOnServer}>اعتبارسنجی انتشار</button>
    </section>

    <div class="routes-layout">
      <section class="route-graph-card">
        <div class="section-heading">
          <div>
            <p class="eyebrow">نمای کلی</p>
            <h2>نمودار مسیر</h2>
          </div>
          <span>{toPersianDigits(schema.flow.edges.length)} اتصال</span>
        </div>
        <FlowCanvas
          {schema}
          {selectedEdgeID}
          onSelectEdge={(id) => (selectedEdgeID = id)}
          onPositionChange={storePosition}
        />
      </section>
      <aside class="route-editor builder-panel">
        <div class="panel-heading">
          <div>
            <span class="step-number">۱</span>
            <h2>قانون مسیر</h2>
          </div>
        </div>
        {#if selectedEdge}
          <div class="inspector-scroll route-inspector">
            <div class="route-summary">
              <strong>{nodeLabel(selectedEdge.source_id)}</strong><span>←</span><strong
                >{nodeLabel(selectedEdge.target_id)}</strong
              >
            </div>
            <div class="field-group">
              <label for="route-source">از صفحه</label><select
                id="route-source"
                bind:value={selectedEdge.source_id}
                disabled={!canEdit}
                onchange={touch}
                >{#each schema.pages as page}<option value={page.id}>{page.title}</option>{/each}</select
              >
            </div>
            <div class="field-group">
              <label for="route-target">به صفحه یا پایان</label><select
                id="route-target"
                bind:value={selectedEdge.target_id}
                disabled={!canEdit}
                onchange={touch}
                ><optgroup label="صفحه‌ها"
                  >{#each schema.pages.filter((page) => page.id !== selectedEdge?.source_id) as page}<option
                      value={page.id}>{page.title}</option
                    >{/each}</optgroup
                ><optgroup label="پایان‌ها"
                  >{#each schema.endings as ending}<option value={ending.id}>{ending.title}</option>{/each}</optgroup
                ></select
              >
            </div>
            <div class="field-group">
              <label for="route-priority">اولویت (عدد کمتر زودتر بررسی می‌شود)</label><input
                id="route-priority"
                type="number"
                min="0"
                bind:value={selectedEdge.priority}
                disabled={!canEdit}
                oninput={touch}
              />
            </div>
            <label class="check-row"
              ><input
                type="checkbox"
                checked={selectedEdge.fallback ?? false}
                disabled={!canEdit}
                onchange={(event) => setFallback(selectedEdge!, event.currentTarget.checked)}
              /><span><strong>مسیر پیش‌فرض</strong><small>اگر هیچ شرطی برقرار نبود، این مسیر اجرا می‌شود.</small></span
              ></label
            >

            {#if !selectedEdge.fallback}
              <section class="inspector-section">
                <div class="section-heading compact">
                  <h3>شرط‌ها</h3>
                  <button
                    class="text-button"
                    type="button"
                    disabled={!canEdit || !eligibleFields.length}
                    onclick={() => addCondition(selectedEdge!)}>+ شرط</button
                  >
                </div>
                <div class="condition-mode">
                  <span>اجرای مسیر وقتی</span><select
                    aria-label="نحوهٔ تطبیق شرط‌ها"
                    bind:value={selectedEdge.condition!.match}
                    disabled={!canEdit}
                    onchange={touch}
                    ><option value="all">همهٔ شرط‌ها</option><option value="any">حداقل یک شرط</option></select
                  ><span>درست باشد.</span>
                </div>
                {#each selectedEdge.condition?.conditions ?? [] as condition, index}
                  {@const conditionField = eligibleFields.find((field) => field.id === condition.field_id)}
                  {@const conditionChoices = fieldOptions(conditionField)}
                  <div class="condition-card">
                    <select
                      aria-label="فیلد شرط"
                      value={condition.field_id}
                      disabled={!canEdit}
                      onchange={(event) => updateConditionField(condition, event.currentTarget.value)}
                      >{#each eligibleFields as field}<option value={field.id}>{field.label}</option>{/each}</select
                    ><select
                      aria-label="عملگر شرط"
                      value={condition.operator}
                      disabled={!canEdit}
                      onchange={(event) => updateConditionOperator(condition, event.currentTarget.value)}
                      >{#each operatorsFor(eligibleFields.find((field) => field.id === condition.field_id) ?? eligibleFields[0]) as operator}<option
                          value={operator.value}>{operator.label}</option
                        >{/each}</select
                    >{#if condition.operator !== 'empty' && condition.operator !== 'not_empty'}
                      {#if (condition.operator === 'contains_any' || condition.operator === 'contains_all') && conditionChoices.length}
                        <select
                          aria-label="مقادیر شرط"
                          multiple
                          size={Math.min(4, conditionChoices.length)}
                          disabled={!canEdit}
                          onchange={(event) => updateConditionSelections(condition, event.currentTarget)}
                          >{#each conditionChoices as option}<option
                              value={option.id}
                              selected={Array.isArray(condition.value) && condition.value.includes(option.id)}
                              >{option.label}</option
                            >{/each}</select
                        >
                      {:else if conditionChoices.length}
                        <select
                          aria-label="مقدار شرط"
                          value={String(condition.value ?? '')}
                          disabled={!canEdit}
                          onchange={(event) => updateConditionValue(condition, event.currentTarget.value)}
                          >{#each conditionChoices as option}<option value={option.id}>{option.label}</option
                            >{/each}</select
                        >
                      {:else if isBoolean(conditionField)}
                        <select
                          aria-label="مقدار شرط"
                          value={String(condition.value ?? false)}
                          disabled={!canEdit}
                          onchange={(event) => updateConditionValue(condition, event.currentTarget.value)}
                          ><option value="true">بله</option><option value="false">خیر</option></select
                        >
                      {:else}
                        <input
                          aria-label="مقدار شرط"
                          value={displayValue(condition.value)}
                          disabled={!canEdit}
                          oninput={(event) => updateConditionValue(condition, event.currentTarget.value)}
                          placeholder={condition.operator === 'between' ? 'کمینه, بیشینه' : 'مقدار'}
                        />
                      {/if}
                    {/if}<button
                      class="icon-button danger-hover"
                      type="button"
                      aria-label="حذف شرط"
                      disabled={!canEdit}
                      onclick={() => removeCondition(selectedEdge!, index)}>×</button
                    >
                  </div>
                {/each}
              </section>
            {/if}

            <section class="inspector-section">
              <div class="section-heading compact">
                <h3>مقداردهی خودکار</h3>
                <button
                  class="text-button"
                  type="button"
                  disabled={!canEdit || !assignableFields().length}
                  onclick={() => addAction(selectedEdge!)}>+ مقداردهی</button
                >
              </div>
              {#each selectedEdge.actions ?? [] as action, index}
                {@const actionTarget = assignableFields(selectedEdge).find(
                  (field) => field.id === action.target_field_id
                )}
                {@const actionChoices = fieldOptions(actionTarget)}
                <div class="action-card">
                  <select
                    aria-label="فیلد مقصد"
                    value={action.target_field_id}
                    disabled={!canEdit}
                    onchange={(event) => updateActionTarget(action, event.currentTarget.value)}
                    >{#each assignableFields(selectedEdge) as field}<option value={field.id}>{field.label}</option
                      >{/each}</select
                  ><select aria-label="حالت مقداردهی" bind:value={action.mode} disabled={!canEdit} onchange={touch}
                    ><option value="replace">جایگزینی</option><option value="if_empty">فقط اگر خالی است</option></select
                  ><select
                    aria-label="منبع مقدار"
                    value={action.source}
                    disabled={!canEdit}
                    onchange={(event) => updateActionSource(action, event.currentTarget.value as 'constant' | 'copy')}
                    ><option value="constant">مقدار ثابت</option><option value="copy">کپی از فیلد</option></select
                  >{#if action.source === 'copy'}<select
                      aria-label="فیلد منبع"
                      bind:value={action.source_field_id}
                      disabled={!canEdit}
                      onchange={touch}
                      >{#each compatibleSourceFields(action) as field}<option value={field.id}>{field.label}</option
                        >{/each}</select
                    >{:else if isMulti(actionTarget) && actionChoices.length}<select
                      aria-label="مقدارهای ثابت"
                      multiple
                      size={Math.min(4, actionChoices.length)}
                      disabled={!canEdit}
                      onchange={(event) => updateActionSelections(action, event.currentTarget)}
                      >{#each actionChoices as option}<option
                          value={option.id}
                          selected={Array.isArray(action.value) && action.value.includes(option.id)}
                          >{option.label}</option
                        >{/each}</select
                    >{:else if actionChoices.length}<select
                      aria-label="مقدار ثابت"
                      value={String(action.value ?? '')}
                      disabled={!canEdit}
                      onchange={(event) => updateActionValue(action, event.currentTarget.value)}
                      >{#each actionChoices as option}<option value={option.id}>{option.label}</option>{/each}</select
                    >{:else if isBoolean(actionTarget)}<select
                      aria-label="مقدار ثابت"
                      value={String(action.value ?? false)}
                      disabled={!canEdit}
                      onchange={(event) => updateActionValue(action, event.currentTarget.value)}
                      ><option value="true">بله</option><option value="false">خیر</option></select
                    >{:else}<input
                      aria-label="مقدار ثابت"
                      value={displayValue(action.value)}
                      disabled={!canEdit}
                      oninput={(event) => updateActionValue(action, event.currentTarget.value)}
                    />{/if}<button
                    class="icon-button danger-hover"
                    type="button"
                    aria-label="حذف مقداردهی"
                    disabled={!canEdit}
                    onclick={() => removeAction(selectedEdge!, index)}>×</button
                  >
                </div>
              {/each}
            </section>
            <button
              class="button danger-outline wide"
              type="button"
              disabled={!canEdit}
              onclick={() => deleteRoute(selectedEdge!)}>حذف این مسیر</button
            >
          </div>
        {:else}<div class="palette-copy"><p>یک اتصال را از نمودار انتخاب کنید یا مسیر تازه بسازید.</p></div>{/if}
      </aside>
    </div>

    <div class="route-bottom-grid">
      <section class="builder-panel route-warnings">
        <div class="panel-heading">
          <div>
            <span class="step-number">۲</span>
            <h2>هشدارهای نمودار</h2>
          </div>
        </div>
        {#if warnings.length}<ul>
            {#each warnings as warning}<li>{warning}</li>{/each}
          </ul>{:else}<div class="empty-compact">
            <strong>هشدار ساختاری دیده نشد</strong><span>برای بررسی نوع شرط‌ها، اعتبارسنجی سرور را نیز اجرا کنید.</span>
          </div>{/if}
      </section>
      <section class="builder-panel route-test">
        <div class="panel-heading">
          <div>
            <span class="step-number">۳</span>
            <h2>آزمایش مسیر</h2>
          </div>
          <button class="button secondary compact-button" type="button" onclick={runTest}>اجرای آزمایش</button>
        </div>
        <p>مقادیر نمونه را وارد کنید؛ برای چندانتخابی و بازه از ویرگول استفاده کنید.</p>
        <div class="test-fields">
          {#each allConditionFields as field}<label
              >{field.label}<input
                value={testAnswers[field.id] ?? ''}
                oninput={(event) => (testAnswers[field.id] = event.currentTarget.value)}
              /></label
            >{/each}
        </div>
        {#if testError}<div class="alert error">{testError}</div>{/if}{#if testedPath.length}<ol class="tested-path">
            {#each testedPath as nodeID}<li>{nodeLabel(nodeID)}</li>{/each}
          </ol>{/if}
      </section>
      <section class="builder-panel endings-editor">
        <div class="panel-heading">
          <div>
            <span class="step-number">۴</span>
            <h2>پایان‌ها</h2>
          </div>
          <button class="icon-button" type="button" aria-label="افزودن پایان" disabled={!canEdit} onclick={addEnding}
            >+</button
          >
        </div>
        <div class="ending-editor-list">
          {#each schema.endings as ending}<article>
              <div class="field-group">
                <label for={`ending-title-${ending.id}`}>عنوان پایان</label><input
                  id={`ending-title-${ending.id}`}
                  bind:value={ending.title}
                  disabled={!canEdit}
                  oninput={touch}
                />
              </div>
              <div class="field-group">
                <label for={`ending-message-${ending.id}`}>پیام</label><textarea
                  id={`ending-message-${ending.id}`}
                  rows="2"
                  bind:value={ending.message}
                  disabled={!canEdit}
                  oninput={touch}></textarea>
              </div>
              <button
                class="icon-button danger-hover"
                type="button"
                aria-label={`حذف ${ending.title}`}
                disabled={!canEdit || schema.endings.length <= 1}
                onclick={() => removeEnding(ending.id)}>×</button
              >
            </article>{/each}
        </div>
      </section>
    </div>
  </div>
{/if}
