import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import Modal from './Modal.svelte';

describe('Modal', () => {
  it('closes a dismissible dialog with Escape', async () => {
    const onClose = vi.fn();
    render(Modal, { title: 'ویرایش', onClose });
    expect(screen.getByRole('button', { name: 'بستن' })).toBeTruthy();
    await fireEvent.keyDown(window, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('keeps a conflict dialog open until an explicit resolution', async () => {
    const onClose = vi.fn();
    render(Modal, { title: 'تداخل در ویرایش', dismissible: false, onClose });
    expect(screen.queryByRole('button', { name: 'بستن' })).toBeNull();
    await fireEvent.keyDown(window, { key: 'Escape' });
    expect(onClose).not.toHaveBeenCalled();
  });
});
