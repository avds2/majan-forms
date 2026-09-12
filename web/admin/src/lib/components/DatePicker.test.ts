import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import DatePicker from './DatePicker.svelte';

describe('DatePicker', () => {
  it('converts a Persian input to an ISO boundary value', async () => {
    const onValueChange = vi.fn();
    render(DatePicker, {
      id: 'date',
      label: 'تاریخ',
      value: '',
      onValueChange
    });
    const input = screen.getByLabelText('تاریخ');
    await fireEvent.input(input, { target: { value: '۱۴۰۵/۰۶/۱۴' } });
    await fireEvent.keyDown(input, { key: 'Enter' });
    expect(onValueChange).toHaveBeenCalledWith('2026-09-05');
    expect((input as HTMLInputElement).value).toBe('۱۴۰۵/۰۶/۱۴');
  });

  it('keeps invalid input visible and announces the error', async () => {
    render(DatePicker, { id: 'date', label: 'تاریخ', value: '' });
    const input = screen.getByLabelText('تاریخ');
    await fireEvent.input(input, { target: { value: '۱۴۰۰/۱۲/۳۰' } });
    await fireEvent.blur(input);
    expect(screen.getByText('تاریخ را به‌شکل سال/ماه/روز وارد کنید.')).toBeTruthy();
    expect(input.getAttribute('aria-invalid')).toBe('true');
  });
});
