<script lang="ts">
  import {
    formatISODate,
    getDisplayTimezone,
    isoToPersianParts,
    monthGrid,
    parsePersianDate,
    persianMonths,
    persianPartsToISO,
    toPersianDigits
  } from '../persian-date';

  export let id: string;
  export let value = '';
  export let label = '';
  export let placeholder = '۱۴۰۵/۰۶/۱۴';
  export let disabled = false;
  export let onValueChange: (iso: string) => void = () => undefined;

  const today = new Intl.DateTimeFormat('en-US-u-ca-persian', {
    timeZone: getDisplayTimezone(),
    year: 'numeric',
    month: 'numeric',
    day: 'numeric'
  }).formatToParts(new Date());
  const currentPart = (type: Intl.DateTimeFormatPartTypes) => Number(today.find((part) => part.type === type)?.value);

  let display = formatISODate(value);
  let open = false;
  let error = '';
  let focused = false;
  let observedValue = value;
  let visibleYear = currentPart('year');
  let visibleMonth = currentPart('month');

  $: if (value !== observedValue) {
    observedValue = value;
    if (!focused) display = formatISODate(value);
    const parts = isoToPersianParts(value);
    if (parts) {
      visibleYear = parts.year;
      visibleMonth = parts.month;
    }
  }

  $: days = monthGrid(visibleYear, visibleMonth);

  function commit() {
    focused = false;
    if (!display.trim()) {
      error = '';
      onValueChange('');
      return;
    }
    const parsed = parsePersianDate(display);
    if (!parsed) {
      error = 'تاریخ را به‌شکل سال/ماه/روز وارد کنید.';
      return;
    }
    error = '';
    display = formatISODate(parsed);
    onValueChange(parsed);
  }

  function showCalendar() {
    if (disabled) return;
    const selected = isoToPersianParts(value);
    if (selected) {
      visibleYear = selected.year;
      visibleMonth = selected.month;
    }
    open = !open;
  }

  function moveMonth(amount: number) {
    visibleMonth += amount;
    if (visibleMonth > 12) {
      visibleMonth = 1;
      visibleYear += 1;
    } else if (visibleMonth < 1) {
      visibleMonth = 12;
      visibleYear -= 1;
    }
  }

  function selectDay(day: number) {
    const iso = persianPartsToISO(visibleYear, visibleMonth, day);
    display = formatISODate(iso);
    error = '';
    open = false;
    onValueChange(iso);
  }

  function handleInputKeydown(event: KeyboardEvent) {
    if (event.key === 'Enter') {
      event.preventDefault();
      commit();
    } else if (event.key === 'ArrowDown' && event.altKey) {
      event.preventDefault();
      showCalendar();
    } else if (event.key === 'Escape') {
      open = false;
    }
  }
</script>

<div class="date-picker">
  {#if label}<label for={id}>{label}</label>{/if}
  <div class:error class="date-input-wrap">
    <input
      {id}
      type="text"
      inputmode="numeric"
      autocomplete="off"
      {placeholder}
      {disabled}
      bind:value={display}
      aria-invalid={error ? 'true' : undefined}
      aria-describedby={error ? `${id}-error` : undefined}
      onfocus={() => (focused = true)}
      onblur={commit}
      onkeydown={handleInputKeydown}
    />
    <button
      type="button"
      class="calendar-trigger"
      aria-label="باز کردن تقویم شمسی"
      aria-expanded={open}
      onclick={showCalendar}
      {disabled}
    >
      <svg viewBox="0 0 24 24" aria-hidden="true"
        ><path d="M7 3v3m10-3v3M4 9h16M5 5h14a1 1 0 0 1 1 1v14H4V6a1 1 0 0 1 1-1Zm3 8h2m4 0h2m-8 4h2m4 0h2" /></svg
      >
    </button>
  </div>
  {#if error}<p class="field-error" id={`${id}-error`}>{error}</p>{/if}
  {#if open}
    <div class="calendar-popover" aria-label="تقویم شمسی">
      <div class="calendar-head">
        <button type="button" aria-label="ماه بعد" onclick={() => moveMonth(1)}>
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 18 6-6-6-6" /></svg>
        </button>
        <strong>{persianMonths[visibleMonth - 1]} {toPersianDigits(visibleYear)}</strong>
        <button type="button" aria-label="ماه قبل" onclick={() => moveMonth(-1)}>
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m15 18-6-6 6-6" /></svg>
        </button>
      </div>
      <div class="calendar-grid calendar-weekdays" aria-hidden="true">
        {#each ['ش', 'ی', 'د', 'س', 'چ', 'پ', 'ج'] as weekday}<span>{weekday}</span>{/each}
      </div>
      <div class="calendar-grid">
        {#each days as day}
          {#if day === null}
            <span></span>
          {:else}
            <button
              type="button"
              class:selected={value === persianPartsToISO(visibleYear, visibleMonth, day)}
              aria-label={`${day} ${persianMonths[visibleMonth - 1]}`}
              onclick={() => selectDay(day)}>{toPersianDigits(day)}</button
            >
          {/if}
        {/each}
      </div>
    </div>
  {/if}
</div>
