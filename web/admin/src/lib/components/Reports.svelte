<script lang="ts">
  import { onMount } from 'svelte';
  import { api, APIError } from '../api';
  import { toPersianDigits } from '../persian-date';
  import type { Project, ProjectReport, ReportQuery } from '../types';
  import DatePicker from './DatePicker.svelte';
  import ProjectTabs from './ProjectTabs.svelte';

  export let projectID: string;
  export let onNavigate: (path: string) => void;

  let project: Project | null = null;
  let report: ProjectReport | null = null;
  let loading = true;
  let reportLoading = false;
  let error = '';
  let version = '';
  let ending = '';
  let fieldID = '';
  let fieldValue = '';
  let from = '';
  let to = '';
  let ChartsComponent: typeof import('./ReportCharts.svelte').default | null = null;
  let chartsLoading = false;

  $: if (report && !ChartsComponent && !chartsLoading) loadCharts();
  $: selectedField = report?.fields.find((item) => item.id === fieldID);

  onMount(load);

  async function load() {
    loading = true;
    error = '';
    try {
      project = await api.project(projectID);
      await loadReport();
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      loading = false;
    }
  }

  function currentQuery(): ReportQuery {
    return {
      version: version ? Number(version) : undefined,
      ending: ending || undefined,
      field: fieldID || undefined,
      field_value: fieldID && fieldValue ? fieldValue : undefined,
      from: from || undefined,
      to: to || undefined
    };
  }

  function changeVersion(value: string) {
    version = value;
    ending = '';
    fieldID = '';
    fieldValue = '';
  }

  function changeField(value: string) {
    fieldID = value;
    fieldValue = '';
  }

  async function loadReport() {
    reportLoading = true;
    error = '';
    try {
      report = await api.report(projectID, currentQuery());
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      reportLoading = false;
    }
  }

  async function loadCharts() {
    chartsLoading = true;
    try {
      ChartsComponent = (await import('./ReportCharts.svelte')).default;
    } finally {
      chartsLoading = false;
    }
  }

  function printReport() {
    window.open(api.printReportURL(projectID, currentQuery()), '_blank', 'noopener,noreferrer');
  }

  function percentage(value: number): string {
    return `${toPersianDigits(value.toFixed(1))}٪`;
  }

  function number(value: number, digits = 0): string {
    return toPersianDigits(
      value.toLocaleString('en-US', { maximumFractionDigits: digits, minimumFractionDigits: digits })
    );
  }

  function messageOf(caught: unknown): string {
    return caught instanceof APIError ? caught.message : 'ارتباط با سرور برقرار نشد.';
  }
</script>

{#if loading}
  <div class="builder-loading">
    <div class="loader"></div>
    <span>در حال آماده‌سازی گزارش‌ها…</span>
  </div>
{:else if !project || !report}
  <main class="startup-error embedded">
    <div class="status-orb error">!</div>
    <h1>گزارش‌ها باز نشد</h1>
    <p>{error}</p>
    <button class="button primary" type="button" onclick={load}>تلاش دوباره</button>
  </main>
{:else}
  <div class="builder-page data-page reports-page">
    <header class="builder-header">
      <div class="builder-title">
        <button class="icon-button ghost" type="button" aria-label="بازگشت به پروژه‌ها" onclick={() => onNavigate('/')}
          ><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 18 6-6-6-6" /></svg></button
        >
        <div>
          <span>پروژه / گزارش‌ها</span>
          <h1>{project.title}</h1>
        </div>
      </div>
      <button class="button secondary" type="button" onclick={printReport}
        ><svg viewBox="0 0 24 24" aria-hidden="true"
          ><path
            d="M7 9V3h10v6M7 17H5a2 2 0 0 1-2-2v-4a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v4a2 2 0 0 1-2 2h-2M7 14h10v7H7z"
          /></svg
        >چاپ A4 / PDF</button
      >
    </header>
    <ProjectTabs {projectID} active="reports" {onNavigate} />
    <main class="data-content report-content">
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
      <form
        class="report-filters"
        onsubmit={(event) => {
          event.preventDefault();
          loadReport();
        }}
      >
        <div class="field-group">
          <label for="report-version">نسخه</label><select
            id="report-version"
            value={version}
            onchange={(event) => changeVersion(event.currentTarget.value)}
            ><option value="">همهٔ نسخه‌ها</option>{#each report.versions as item}<option value={item.version_number}
                >نسخهٔ {toPersianDigits(item.version_number)}</option
              >{/each}</select
          >
        </div>
        <div class="field-group">
          <label for="report-ending">پایان</label><select id="report-ending" bind:value={ending}
            ><option value="">همهٔ پایان‌ها</option>{#each report.ending_options as item}<option value={item.id}
                >{item.label}</option
              >{/each}</select
          >
        </div>
        <div class="field-group">
          <label for="report-field">پاسخ فیلد</label><select
            id="report-field"
            value={fieldID}
            onchange={(event) => changeField(event.currentTarget.value)}
            ><option value="">همهٔ پاسخ‌ها</option>{#each report.fields as item}<option value={item.id}
                >{item.label}</option
              >{/each}</select
          >
        </div>
        {#if selectedField}
          <div class="field-group">
            <label for="report-field-value">مقدار پاسخ</label>
            {#if selectedField.options?.length}<select id="report-field-value" bind:value={fieldValue}
                ><option value="">هر مقدار ثبت‌شده</option>{#each selectedField.options as option}<option
                    value={option.id}>{option.label}</option
                  >{/each}</select
              >{:else if selectedField.type === 'yes_no' || selectedField.type === 'consent'}<select
                id="report-field-value"
                bind:value={fieldValue}
                ><option value="">هر مقدار ثبت‌شده</option><option value="true">بله</option><option value="false"
                  >خیر</option
                ></select
              >{:else}<input
                id="report-field-value"
                bind:value={fieldValue}
                maxlength="500"
                placeholder="خالی یعنی پاسخ‌داده‌شده"
              />{/if}
          </div>
        {/if}
        <DatePicker
          id="report-from"
          value={from}
          label="از تاریخ"
          onValueChange={(value) => (from = value)}
        /><DatePicker id="report-to" value={to} label="تا تاریخ" onValueChange={(value) => (to = value)} /><button
          class="button primary"
          type="submit"
          disabled={reportLoading}>به‌روزرسانی گزارش</button
        >
      </form>

      {#if reportLoading}<div class="table-loading">
          <div class="loader"></div>
          <span>در حال محاسبهٔ گزارش…</span>
        </div>{:else}
        <section class="report-kpis" aria-label="شاخص‌های اصلی">
          <article>
            <span>بازدید فرم</span><strong>{number(report.kpi.views)}</strong><small>نمایش صفحهٔ آغاز</small>
          </article>
          <article>
            <span>شروع پاسخ</span><strong>{number(report.kpi.starts)}</strong><small>نشست‌های آغازشده</small>
          </article>
          <article>
            <span>پاسخ کامل</span><strong>{number(report.kpi.completes)}</strong><small>بدون پاسخ‌های حذف‌شده</small>
          </article>
          <article class:accent={!report.segmented}>
            <span>نرخ تکمیل</span><strong>{report.segmented ? '—' : percentage(report.kpi.completion_rate)}</strong
            ><small>{report.segmented ? 'برای فیلتر محتوایی قابل محاسبه نیست' : 'از شروع تا ثبت نهایی'}</small>
          </article>
          <article>
            <span>میانگین زمان</span><strong>{number(report.kpi.average_duration_seconds, 1)}</strong><small
              >ثانیه تا تکمیل</small
            >
          </article>
        </section>
        {#if report.segmented}<div class="alert info" role="status">
            فیلتر پایان یا پاسخ فقط روی پاسخ‌های تکمیل‌شده اعمال می‌شود؛ بازدید، شروع و فعالیت صفحه‌ها کل ترافیک بازه را
            نشان می‌دهند.
          </div>{/if}
        {#if ChartsComponent}<ChartsComponent
            trend={report.trend}
            endings={report.endings}
            segmented={report.segmented}
          />{:else}<div class="table-loading compact">
            <div class="loader"></div>
            <span>در حال رسم نمودارها…</span>
          </div>{/if}

        <div class="report-detail-grid">
          <section class="report-card">
            <div class="report-card-heading">
              <div>
                <span>پراکندگی پاسخ</span>
                <h2>گزینه‌های انتخابی</h2>
              </div>
            </div>
            {#if report.choices.length}<div class="choice-reports">
                {#each report.choices as field}<article>
                    <header>
                      <strong>{field.field_label}</strong><span
                        >{field.across_versions
                          ? 'همهٔ نسخه‌ها'
                          : `نسخهٔ ${toPersianDigits(field.version_number)}`}</span
                      >
                    </header>
                    {#each field.values as item}<div class="choice-value">
                        <div><span>{item.label}</span><strong>{toPersianDigits(item.count)}</strong></div>
                        <progress max={Math.max(1, ...field.values.map((value) => value.count))} value={item.count}
                          >{item.count}</progress
                        >
                      </div>{/each}
                  </article>{/each}
              </div>{:else}<div class="report-empty">پرسش انتخابیِ پاسخ‌داده‌شده‌ای وجود ندارد.</div>{/if}
          </section>
          <section class="report-card">
            <div class="report-card-heading">
              <div>
                <span>خلاصهٔ آماری</span>
                <h2>پرسش‌های عددی</h2>
              </div>
            </div>
            {#if report.numbers.length}<div class="responsive-table">
                <table>
                  <thead
                    ><tr
                      ><th>پرسش</th><th>نسخه</th><th>تعداد</th><th>کمینه</th><th>میانه</th><th>میانگین</th><th
                        >بیشینه</th
                      ><th>NPS</th></tr
                    ></thead
                  ><tbody
                    >{#each report.numbers as item}<tr
                        ><td data-label="پرسش">{item.field_label}</td><td data-label="نسخه"
                          >{item.across_versions ? 'همه' : toPersianDigits(item.version_number)}</td
                        ><td data-label="تعداد">{number(item.count)}</td><td data-label="کمینه"
                          >{number(item.minimum, 2)}</td
                        ><td data-label="میانه">{number(item.median, 2)}</td><td data-label="میانگین"
                          >{number(item.average, 2)}</td
                        ><td data-label="بیشینه">{number(item.maximum, 2)}</td><td data-label="NPS"
                          >{item.nps === undefined ? '—' : number(item.nps, 1)}</td
                        ></tr
                      >{/each}</tbody
                  >
                </table>
              </div>{:else}<div class="report-empty">پاسخ عددی‌ای وجود ندارد.</div>{/if}
          </section>
        </div>

        <section class="report-card page-activity">
          <div class="report-card-heading">
            <div>
              <span>حرکت در فرم</span>
              <h2>فعالیت صفحه‌ها{report.segmented ? ' · کل ترافیک بازه' : ''}</h2>
            </div>
          </div>
          {#if report.page_activity.length}<div class="responsive-table">
              <table>
                <thead><tr><th>صفحه</th><th>نسخه</th><th>عبور ثبت‌شده</th></tr></thead><tbody
                  >{#each report.page_activity as item}<tr
                      ><td data-label="صفحه">{item.label}</td><td data-label="نسخه"
                        >{item.across_versions ? 'همهٔ نسخه‌ها' : `نسخهٔ ${toPersianDigits(item.version_number)}`}</td
                      ><td data-label="عبور">{number(item.count)}</td></tr
                    >{/each}</tbody
                >
              </table>
            </div>{:else}<div class="report-empty">هنوز عبور از صفحه‌ای ثبت نشده است.</div>{/if}
        </section>
        <p class="report-generated">آخرین محاسبه: {report.generated_at_jalali}</p>
      {/if}
    </main>
  </div>
{/if}
