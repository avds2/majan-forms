import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import {
  formatISODate,
  getDisplayTimezone,
  monthGrid,
  normalizeDigits,
  parsePersianDate,
  setDisplayTimezone
} from './persian-date';

const sharedCases = JSON.parse(
  readFileSync(resolve(process.cwd(), '../../testdata/persian-date-cases.json'), 'utf8')
) as Array<{ persian: string; iso: string }>;

describe('Persian dates', () => {
  it('accepts Persian, Arabic and Latin digits', () => {
    expect(normalizeDigits('۱۴۰۵/٠٦/14')).toBe('1405/06/14');
    expect(parsePersianDate('۱۴۰۵/۰۶/۱۴')).toBe('2026-09-05');
    expect(parsePersianDate('1403-01-01')).toBe('2024-03-20');
  });

  it('round trips the fixture shared with the Go server', () => {
    for (const testCase of sharedCases) {
      expect(parsePersianDate(testCase.persian)).toBe(testCase.iso);
      expect(parsePersianDate(formatISODate(testCase.iso))).toBe(testCase.iso);
    }
  });

  it('round trips the ISO boundary representation', () => {
    expect(formatISODate('2026-09-05')).toBe('۱۴۰۵/۰۶/۱۴');
    expect(formatISODate('2026-02-31')).toBe('');
  });

  it('rejects the last day of Esfand in a non-leap year', () => {
    expect(parsePersianDate('۱۴۰۰/۱۲/۳۰')).toBeNull();
  });

  it('starts calendar weeks on Saturday', () => {
    const grid = monthGrid(1403, 1);
    expect(grid.slice(0, 4)).toEqual([null, null, null, null]);
    expect(grid[4]).toBe(1);
  });

  it('uses the validated runtime timezone', () => {
    setDisplayTimezone('UTC');
    expect(getDisplayTimezone()).toBe('UTC');
    expect(() => setDisplayTimezone('not/a-timezone')).toThrow();
    expect(getDisplayTimezone()).toBe('UTC');
    setDisplayTimezone('Asia/Tehran');
  });
});
