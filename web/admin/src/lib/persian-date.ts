import { isValidJalaaliDate, jalaaliMonthLength, toGregorian, toJalaali } from 'jalaali-js';

let displayTimezone = 'Asia/Tehran';

export function setDisplayTimezone(timezone: string): void {
  new Intl.DateTimeFormat('fa-IR', { timeZone: timezone }).format(new Date(0));
  displayTimezone = timezone;
}

export function getDisplayTimezone(): string {
  return displayTimezone;
}

export const persianMonths = [
  'فروردین',
  'اردیبهشت',
  'خرداد',
  'تیر',
  'مرداد',
  'شهریور',
  'مهر',
  'آبان',
  'آذر',
  'دی',
  'بهمن',
  'اسفند'
] as const;

const latinDigits = new Map([
  ['۰', '0'],
  ['۱', '1'],
  ['۲', '2'],
  ['۳', '3'],
  ['۴', '4'],
  ['۵', '5'],
  ['۶', '6'],
  ['۷', '7'],
  ['۸', '8'],
  ['۹', '9'],
  ['٠', '0'],
  ['١', '1'],
  ['٢', '2'],
  ['٣', '3'],
  ['٤', '4'],
  ['٥', '5'],
  ['٦', '6'],
  ['٧', '7'],
  ['٨', '8'],
  ['٩', '9']
]);

export function normalizeDigits(value: string): string {
  return [...value].map((character) => latinDigits.get(character) ?? character).join('');
}

export function toPersianDigits(value: string | number): string {
  return String(value).replace(/\d/g, (digit) => '۰۱۲۳۴۵۶۷۸۹'[Number(digit)]);
}

export function parsePersianDate(value: string): string | null {
  const parts = normalizeDigits(value.trim())
    .replace(/[.\\-]/g, '/')
    .split('/');
  if (parts.length !== 3) return null;
  const [jy, jm, jd] = parts.map(Number);
  if (![jy, jm, jd].every(Number.isInteger) || !isValidJalaaliDate(jy, jm, jd)) return null;
  const { gy, gm, gd } = toGregorian(jy, jm, jd);
  return `${gy.toString().padStart(4, '0')}-${gm.toString().padStart(2, '0')}-${gd.toString().padStart(2, '0')}`;
}

export function isoToPersianParts(iso: string): { year: number; month: number; day: number } | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  if (!match) return null;
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const date = new Date(Date.UTC(year, month - 1, day));
  if (Number.isNaN(date.valueOf())) return null;
  if (date.getUTCFullYear() !== year || date.getUTCMonth() + 1 !== month || date.getUTCDate() !== day) return null;
  try {
    const { jy, jm, jd } = toJalaali(date.getUTCFullYear(), date.getUTCMonth() + 1, date.getUTCDate());
    return { year: jy, month: jm, day: jd };
  } catch {
    return null;
  }
}

export function formatISODate(iso?: string): string {
  if (!iso) return '';
  const parts = isoToPersianParts(iso);
  if (!parts) return '';
  return toPersianDigits(`${parts.year}/${String(parts.month).padStart(2, '0')}/${String(parts.day).padStart(2, '0')}`);
}

export function formatDateTime(iso?: string): string {
  if (!iso) return '—';
  const value = new Date(iso);
  if (Number.isNaN(value.valueOf())) return '—';
  return new Intl.DateTimeFormat('fa-IR-u-ca-persian', {
    timeZone: displayTimezone,
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  }).format(value);
}

export function monthGrid(year: number, month: number): Array<number | null> {
  const first = toGregorian(year, month, 1);
  const weekday = (new Date(Date.UTC(first.gy, first.gm - 1, first.gd)).getUTCDay() + 1) % 7;
  const days = Array.from({ length: jalaaliMonthLength(year, month) }, (_, index) => index + 1);
  return [...Array<number | null>(weekday).fill(null), ...days];
}

export function persianPartsToISO(year: number, month: number, day: number): string {
  const { gy, gm, gd } = toGregorian(year, month, day);
  return `${gy.toString().padStart(4, '0')}-${gm.toString().padStart(2, '0')}-${gd.toString().padStart(2, '0')}`;
}
