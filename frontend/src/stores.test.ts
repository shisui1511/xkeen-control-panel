import { describe, it, expect, vi, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import { confirmStore, showConfirm, toastStore, showToast } from './stores';

describe('stores - ConfirmDialog & Toast', () => {
  beforeEach(() => {
    confirmStore.set(null);
    toastStore.set([]);
  });

  it('showConfirm opens confirmation store and returns a promise resolving to boolean', async () => {
    const promise = showConfirm({
      title: 'Удалить подписку?',
      message: 'Вы уверены?',
      variant: 'danger',
      confirmLabel: 'Удалить'
    });

    const currentReq = get(confirmStore);
    expect(currentReq).not.toBeNull();
    expect(currentReq?.title).toBe('Удалить подписку?');
    expect(currentReq?.variant).toBe('danger');

    // Simulate clicking confirm
    currentReq?.resolve(true);
    const result = await promise;
    expect(result).toBe(true);
  });

  it('showConfirm resolves to false when cancelled', async () => {
    const promise = showConfirm('Подтвердите действие', 'Сообщение');

    const currentReq = get(confirmStore);
    expect(currentReq).not.toBeNull();
    expect(currentReq?.variant).toBe('danger'); // default variant

    // Simulate clicking cancel / Escape / overlay click
    currentReq?.resolve(false);
    const result = await promise;
    expect(result).toBe(false);
  });

  it('showToast adds item to toastStore and auto-dismisses after duration', () => {
    vi.useFakeTimers();

    showToast('success', 'Сохранено', 3000);
    let items = get(toastStore);
    expect(items.length).toBe(1);
    expect(items[0].message).toBe('Сохранено');
    expect(items[0].type).toBe('success');

    vi.advanceTimersByTime(3000);
    items = get(toastStore);
    expect(items.length).toBe(0);

    vi.useRealTimers();
  });
});
