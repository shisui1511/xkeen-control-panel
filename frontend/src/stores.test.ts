import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';
import { confirmStore, showConfirm, toastStore, showToast, panelUnreachable } from './stores';

describe('stores - ConfirmDialog & Toast', () => {
  beforeEach(() => {
    confirmStore.set(null);
    toastStore.set([]);
    panelUnreachable.set(false);
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

  it('showToast suppresses error toasts while panelUnreachable, but not warning/success (D-20)', () => {
    panelUnreachable.set(true);

    showToast('error', 'Сеть недоступна');
    expect(get(toastStore)).toHaveLength(0);

    showToast('warning', 'Внимание');
    expect(get(toastStore)).toHaveLength(1);

    showToast('success', 'Готово');
    expect(get(toastStore)).toHaveLength(2);

    panelUnreachable.set(false);
    showToast('error', 'Снова доступно для тоста');
    expect(get(toastStore)).toHaveLength(3);
  });
});

// Модуль stores держит состояние на уровне модуля (счётчик замков, lastValidActiveKernel),
// поэтому каждый тест заново импортирует его после vi.resetModules() (как serviceApply.test.ts).
describe('navCaps и lockNav', () => {
  const apiFetchJSON = vi.fn();
  const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

  const caps = (active: string) => ({
    kernels: {
      xray: { installed: true },
      mihomo: { installed: true }
    },
    active_kernel: active,
    xkeen_installed: true,
    mihomo: { reachable: true, process_running: false, api_reachable: false }
  });

  async function load(initial?: string) {
    vi.doMock('./lib/api', () => ({ apiFetchJSON }));
    vi.stubGlobal('localStorage', {
      getItem: vi.fn().mockReturnValue(initial ?? null),
      setItem: vi.fn(),
      removeItem: vi.fn()
    });
    return await import('./stores');
  }

  beforeEach(() => {
    vi.resetModules();
    apiFetchJSON.mockReset();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.doUnmock('./lib/api');
  });

  const xrayCache = JSON.stringify({
    v: 1,
    active_kernel: 'xray',
    kernels: { xray: true, mihomo: true }
  });

  it('lockNav: ответ mihomo не меняет navCaps, capabilities обновляется; снятие даёт один запрос', async () => {
    const { lockNav, fetchCapabilities, navCaps, capabilities } = await load(xrayCache);
    expect(get(navCaps)?.active_kernel).toBe('xray');

    const unlock = lockNav();
    apiFetchJSON.mockResolvedValue(caps('mihomo'));
    await fetchCapabilities();
    expect(get(navCaps)?.active_kernel).toBe('xray');
    expect(get(capabilities)?.active_kernel).toBe('mihomo');
    expect(apiFetchJSON).toHaveBeenCalledTimes(1);

    unlock();
    await flush();
    expect(apiFetchJSON).toHaveBeenCalledTimes(2);
    expect(apiFetchJSON.mock.calls[1][0]).toBe('/api/capabilities');
    expect(get(navCaps)?.active_kernel).toBe('mihomo');

    // Повторный вызов той же функции снятия ничего не делает
    unlock();
    await flush();
    expect(apiFetchJSON).toHaveBeenCalledTimes(2);
  });

  it('два замка: снятие первого без запроса, снятие второго — один запрос', async () => {
    const { lockNav, navLockCount } = await load(xrayCache);
    apiFetchJSON.mockResolvedValue(caps('xray'));

    const first = lockNav();
    const second = lockNav();
    expect(get(navLockCount)).toBe(2);

    first();
    await flush();
    expect(get(navLockCount)).toBe(1);
    expect(apiFetchJSON).not.toHaveBeenCalled();

    second();
    await flush();
    expect(get(navLockCount)).toBe(0);
    expect(apiFetchJSON).toHaveBeenCalledTimes(1);
  });

  it('none при известном кэше не меняет меню, при пустом кэше — виден как none', async () => {
    const known = await load(xrayCache);
    apiFetchJSON.mockResolvedValue(caps('none'));
    await known.fetchCapabilities();
    expect(get(known.navCaps)?.active_kernel).toBe('xray');

    vi.resetModules();
    const cold = await load();
    expect(get(cold.navCaps)).toBeNull();
    await cold.fetchCapabilities();
    expect(get(cold.navCaps)?.active_kernel).toBe('none');
  });

  it('настоящая смена ядра xray → mihomo применяется сразу без замка', async () => {
    const { fetchCapabilities, navCaps } = await load(xrayCache);
    apiFetchJSON.mockResolvedValue(caps('mihomo'));
    await fetchCapabilities();
    expect(get(navCaps)?.active_kernel).toBe('mihomo');
  });

  it('ответ без пригодного активного ядра при пустом кэше не оставляет скелетон', async () => {
    const { fetchCapabilities, navCaps } = await load();
    apiFetchJSON.mockResolvedValue({ mihomo: { reachable: true } });
    await fetchCapabilities();
    expect(get(navCaps)).not.toBeNull();
  });

  it('localStorage.getItem бросает — readNavCaps возвращает null', async () => {
    vi.doMock('./lib/api', () => ({ apiFetchJSON }));
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('denied');
      },
      setItem: vi.fn()
    });
    const { readNavCaps, navCaps } = await import('./stores');
    expect(readNavCaps()).toBeNull();
    expect(get(navCaps)).toBeNull();
  });

  it('localStorage.setItem бросает — fetchCapabilities завершается без исключения', async () => {
    vi.doMock('./lib/api', () => ({ apiFetchJSON }));
    vi.stubGlobal('localStorage', {
      getItem: vi.fn().mockReturnValue(null),
      setItem: () => {
        throw new Error('quota');
      }
    });
    const { fetchCapabilities, navCaps } = await import('./stores');
    apiFetchJSON.mockResolvedValue(caps('xray'));
    await expect(fetchCapabilities()).resolves.toBeUndefined();
    expect(get(navCaps)?.active_kernel).toBe('xray');
  });

  it('в localStorage пишется только срез без секретов', async () => {
    const { fetchCapabilities } = await load();
    const setItem = (globalThis.localStorage as unknown as { setItem: ReturnType<typeof vi.fn> })
      .setItem;
    apiFetchJSON.mockResolvedValue({
      ...caps('xray'),
      global_hwid: 'hwid-1',
      mihomo: { reachable: true, discovered_secret: 'secret-1' }
    });
    await fetchCapabilities();
    const call = setItem.mock.calls.find((c) => c[0] === 'xcp_nav_caps');
    expect(call).toBeTruthy();
    expect(call![1]).not.toContain('secret-1');
    expect(call![1]).not.toContain('hwid-1');
    expect(Object.keys(JSON.parse(call![1])).sort()).toEqual([
      'active_kernel',
      'kernels',
      'v',
      'xkeen_installed'
    ]);
  });
});
