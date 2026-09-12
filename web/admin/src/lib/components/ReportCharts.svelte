<script lang="ts">
  import { toPersianDigits } from '../persian-date';
  import type { CountBreakdown, TrendPoint } from '../types';

  export let trend: TrendPoint[];
  export let endings: CountBreakdown[];
  export let segmented = false;

  const width = 860;
  const height = 250;
  const inset = { right: 42, left: 42, top: 24, bottom: 44 };
  $: maximum = Math.max(
    1,
    ...trend.flatMap((point) => (segmented ? [point.completes] : [point.views, point.starts, point.completes]))
  );
  $: endingMaximum = Math.max(1, ...endings.map((item) => item.count));

  function x(index: number): number {
    if (trend.length <= 1) return width / 2;
    return inset.right + (index * (width - inset.right - inset.left)) / (trend.length - 1);
  }

  function y(value: number): number {
    return inset.top + ((maximum - value) * (height - inset.top - inset.bottom)) / maximum;
  }

  function line(key: 'views' | 'starts' | 'completes'): string {
    return trend
      .map((point, index) => `${index ? 'L' : 'M'}${x(index).toFixed(1)} ${y(point[key]).toFixed(1)}`)
      .join(' ');
  }

  function showLabel(index: number): boolean {
    const step = Math.max(1, Math.ceil(trend.length / 7));
    return index % step === 0 || index === trend.length - 1;
  }
</script>

<div class="report-charts">
  <section class="report-card trend-card">
    <div class="report-card-heading">
      <div>
        <span>رفتار پاسخ‌دهندگان</span>
        <h2>{segmented ? 'روند تکمیل پاسخ‌های فیلترشده' : 'روند بازدید تا تکمیل'}</h2>
      </div>
      <div class="chart-legend">
        {#if !segmented}<span class="views">بازدید</span><span class="starts">شروع</span>{/if}<span class="completes"
          >تکمیل</span
        >
      </div>
    </div>
    {#if trend.length}
      <div class="trend-visual">
        <svg
          class="trend-svg"
          viewBox={`0 0 ${width} ${height}`}
          role="img"
          aria-label={segmented ? 'روند تکمیل پاسخ‌های فیلترشده' : 'روند بازدید، شروع و تکمیل فرم'}
          ><line
            class="chart-axis"
            x1={inset.right}
            y1={height - inset.bottom}
            x2={width - inset.left}
            y2={height - inset.bottom}
          /><line
            class="chart-grid"
            x1={inset.right}
            y1={y(maximum / 2)}
            x2={width - inset.left}
            y2={y(maximum / 2)}
          />{#if !segmented}<path class="chart-line views" d={line('views')} /><path
              class="chart-line starts"
              d={line('starts')}
            />{/if}<path class="chart-line completes" d={line('completes')} />{#each trend as point, index}<circle
              class="chart-dot completes"
              cx={x(index)}
              cy={y(point.completes)}
              r="3"
            />{/each}</svg
        >
        <div class="chart-date-labels" aria-hidden="true">
          {#each trend as point, index}{#if showLabel(index)}<span dir="ltr">{point.date_jalali}</span>{/if}{/each}
        </div>
      </div>
    {:else}<div class="report-empty">برای این بازه هنوز رویدادی ثبت نشده است.</div>{/if}
  </section>

  <section class="report-card endings-card">
    <div class="report-card-heading">
      <div>
        <span>نتیجهٔ مسیرها</span>
        <h2>پایان‌های فرم</h2>
      </div>
    </div>
    {#if endings.length}
      <div class="ending-bars">
        {#each endings as item}<div class="ending-row">
            <div>
              <strong>{item.label}</strong><small
                >{item.across_versions ? 'همهٔ نسخه‌ها' : `نسخهٔ ${toPersianDigits(item.version_number)}`}</small
              >
            </div>
            <svg viewBox="0 0 320 24" role="img" aria-label={`${item.label}: ${item.count}`}
              ><rect class="ending-track" x="0" y="3" width="320" height="18" rx="7" /><rect
                class="ending-fill"
                x="0"
                y="3"
                width={(item.count / endingMaximum) * 320}
                height="18"
                rx="7"
              /></svg
            ><span>{toPersianDigits(item.count)}</span>
          </div>{/each}
      </div>
    {:else}<div class="report-empty">هنوز پاسخی به پایان فرم نرسیده است.</div>{/if}
  </section>
</div>
