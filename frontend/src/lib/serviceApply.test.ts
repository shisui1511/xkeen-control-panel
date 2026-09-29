import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';

// Модуль тянет ../stores и ../i18n, у которых есть состояние на уровне модуля:
// чтобы читать тот же toastStore, в который пишет serviceApply, каждый тест
// заново импортирует всё после vi.resetModules() (как в api.test.ts).
const apiFetchJSON = vi.fn();
const apiFetch = vi.fn();

const startMihomo = vi.fn();

async function load() {
  vi.doMock('./api', () => ({ apiFetch, apiFetchJSON, startMihomo }));
  const mod = await import('./serviceApply');
  const stores = await import('../stores');
  const i18n = await import('../i18n');
  const mihomo = await import('./constructors/mihomoApply');
  return {
    ...mod,
    finishMihomoApply: mihomo.finishMihomoApply,
    toastStore: stores.toastStore,
    confirmStore: stores.confirmStore,
    t: i18n.t
  };
}

beforeEach(() => {
  vi.resetModules();
  vi.useFakeTimers();
  apiFetchJSON.mockReset();
  apiFetch.mockReset();
  startMihomo.mockReset();
  // fetchCapabilities после исхода: ответ не важен, важно, что он не падает
  apiFetchJSON.mockResolvedValue({});
  vi.stubGlobal('localStorage', {
    getItem: vi.fn().mockReturnValue('ru'),
    setItem: vi.fn(),
    removeItem: vi.fn()
  });
  vi.stubGlobal('window', { location: { hash: '' } });
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.doUnmock('./api');
});

describe('notifyApplyOutcome', () => {
  it('restarted: success с переданным текстом', async () => {
    const { notifyApplyOutcome, toastStore } = await load();
    notifyApplyOutcome({ outcome: 'restarted', kernel: 'xray' }, { restartedMessage: 'Готово' });
    const [toast] = get(toastStore);
    expect(toast.type).toBe('success');
    expect(toast.message).toBe('Готово');
    expect(toast.action).toBeUndefined();
  });

  it('restarted без текста: success apply.restarted', async () => {
    const { notifyApplyOutcome, toastStore, t } = await load();
    notifyApplyOutcome({ outcome: 'restarted', kernel: 'xray' });
    expect(get(toastStore)[0].message).toBe(get(t)('apply.restarted'));
  });

  it('saved_kernel_stopped: info с действием «Запустить сейчас»', async () => {
    const { notifyApplyOutcome, toastStore, t } = await load();
    notifyApplyOutcome({ outcome: 'saved_kernel_stopped', kernel: 'xray' });
    const [toast] = get(toastStore);
    expect(toast.type).toBe('info');
    expect(toast.message).toBe(get(t)('apply.saved_kernel_stopped'));
    expect(toast.duration).toBe(8000);
    expect(toast.action?.label).toBe(get(t)('apply.start_now'));
  });

  it('saved_kernel_inactive: info без действия, с именем ядра', async () => {
    const { notifyApplyOutcome, toastStore, t } = await load();
    notifyApplyOutcome({ outcome: 'saved_kernel_inactive', kernel: 'mihomo' });
    const [toast] = get(toastStore);
    expect(toast.type).toBe('info');
    expect(toast.message).toBe(get(t)('apply.saved_kernel_inactive', { kernel: 'Mihomo' }));
    expect(toast.message).toContain('Mihomo');
    expect(toast.action).toBeUndefined();
  });

  it('restart_failed: error с причиной и действием «Логи»', async () => {
    const { notifyApplyOutcome, toastStore, t } = await load();
    notifyApplyOutcome({ outcome: 'restart_failed', kernel: 'xray', error: 'port 1181 busy' });
    const [toast] = get(toastStore);
    expect(toast.type).toBe('error');
    expect(toast.message).toContain('port 1181 busy');
    expect(toast.action?.label).toBe(get(t)('apply.open_logs'));
    toast.action?.onClick();
    expect(window.location.hash).toBe('#/logs');
  });

  it('restart_failed без текста ошибки: причина неизвестна', async () => {
    const { notifyApplyOutcome, toastStore, t } = await load();
    notifyApplyOutcome({ outcome: 'restart_failed', kernel: 'xray' });
    expect(get(toastStore)[0].message).toContain(get(t)('apply.restart_failed_unknown'));
  });

  it('unknown: нейтральный success app.saved без утверждения о рестарте', async () => {
    const { notifyApplyOutcome, toastStore, t } = await load();
    notifyApplyOutcome({ outcome: 'unknown', kernel: 'xray' });
    const [toast] = get(toastStore);
    expect(toast.type).toBe('success');
    expect(toast.message).toBe(get(t)('app.saved'));
  });
});

describe('applyToKernel', () => {
  it('kernel: POST action=apply&kernel=…', async () => {
    apiFetchJSON.mockResolvedValue({
      outcome: 'restarted',
      kernel: 'xray',
      active_kernel: 'xray',
      active_running: true
    });
    const { applyToKernel } = await load();
    const result = await applyToKernel({ kernel: 'xray' });
    expect(apiFetchJSON).toHaveBeenCalledWith('/api/service/control?action=apply&kernel=xray', {
      method: 'POST'
    });
    expect(result).toMatchObject({ outcome: 'restarted', kernel: 'xray', active_running: true });
  });

  it('path: путь кодируется', async () => {
    apiFetchJSON.mockResolvedValue({ outcome: 'saved_kernel_stopped', kernel: 'mihomo' });
    const { applyToKernel } = await load();
    await applyToKernel({ path: '/opt/etc/mihomo/my config.yaml' });
    expect(apiFetchJSON).toHaveBeenCalledWith(
      '/api/service/control?action=apply&path=%2Fopt%2Fetc%2Fmihomo%2Fmy%20config.yaml',
      { method: 'POST' }
    );
  });

  it('неизвестный или отсутствующий outcome даёт unknown', async () => {
    const { applyToKernel } = await load();
    apiFetchJSON.mockResolvedValueOnce({ outcome: 'exploded', kernel: 'xray' });
    expect((await applyToKernel({ kernel: 'xray' })).outcome).toBe('unknown');
    apiFetchJSON.mockResolvedValueOnce(null);
    expect((await applyToKernel({ kernel: 'xray' })).outcome).toBe('unknown');
  });

  it('ошибка HTTP пробрасывается вызывающему', async () => {
    apiFetchJSON.mockRejectedValue(new Error('HTTP 500'));
    const { applyToKernel } = await load();
    await expect(applyToKernel({ kernel: 'xray' })).rejects.toThrow('HTTP 500');
  });
});

describe('willRestartOnApply', () => {
  it('true только при явном true из apply_restarts', async () => {
    const { willRestartOnApply } = await load();
    expect(willRestartOnApply({ apply_restarts: { xray: true } }, 'xray')).toBe(true);
    expect(willRestartOnApply({ apply_restarts: { xray: true } }, 'mihomo')).toBe(false);
    expect(willRestartOnApply({ apply_restarts: { xray: false } }, 'xray')).toBe(false);
    expect(willRestartOnApply({}, 'xray')).toBe(false);
    expect(willRestartOnApply(null, 'xray')).toBe(false);
    expect(willRestartOnApply(undefined, 'xray')).toBe(false);
  });
});

describe('finishMihomoApply', () => {
  function lastToast(store: any) {
    const items = get(store) as any[];
    return items[items.length - 1];
  }

  const inactiveRunning = {
    outcome: 'saved_kernel_inactive' as const,
    kernel: 'mihomo',
    active_kernel: 'xray',
    active_running: true
  };

  /** Ждёт диалог переключения и отвечает на него. */
  async function answer(confirmStore: any, value: boolean) {
    await vi.waitFor(() => expect(get(confirmStore)).not.toBeNull());
    const request = get(confirmStore) as any;
    request.resolve(value);
    return request;
  }

  it('Xray работает: диалог с «Переключить и запустить» / «Только сохранить»', async () => {
    const { finishMihomoApply, confirmStore, t } = await load();
    const done = finishMihomoApply(inactiveRunning);
    const request = await answer(confirmStore, false);
    await done;
    expect(request.confirmLabel).toBe(get(t)('apply.switch_and_start'));
    expect(request.cancelLabel).toBe(get(t)('apply.save_only'));
    expect(request.consequence).toBe(get(t)('apply.switch_mihomo_body'));
  });

  it('«Только сохранить»: ни switch_kernel, ни restart, тост без действия', async () => {
    const { finishMihomoApply, confirmStore, toastStore, t } = await load();
    const done = finishMihomoApply(inactiveRunning);
    await answer(confirmStore, false);
    await done;
    expect(startMihomo).not.toHaveBeenCalled();
    expect(apiFetch).not.toHaveBeenCalled();
    const toast = lastToast(toastStore);
    expect(toast.message).toBe(get(t)('apply.mihomo_saved_after_switch'));
    expect(toast.action).toBeUndefined();
  });

  it('согласие: ровно один switch_kernel и тост успеха', async () => {
    startMihomo.mockResolvedValue(undefined);
    const { finishMihomoApply, confirmStore, toastStore } = await load();
    const done = finishMihomoApply(inactiveRunning, 'Готово');
    await answer(confirmStore, true);
    await done;
    expect(startMihomo).toHaveBeenCalledTimes(1);
    const toast = lastToast(toastStore);
    expect(toast.type).toBe('success');
    expect(toast.message).toBe('Готово');
  });

  it('ошибка переключения: тост apply.switch_failed с причиной', async () => {
    startMihomo.mockRejectedValue(new Error('boom'));
    const { finishMihomoApply, confirmStore, toastStore, t } = await load();
    const done = finishMihomoApply(inactiveRunning);
    await answer(confirmStore, true);
    await done;
    const toast = lastToast(toastStore);
    expect(toast.type).toBe('error');
    expect(toast.message).toBe(get(t)('apply.switch_failed', { reason: 'boom' }));
  });

  it('Xray не запущен: диалога нет, тот же тост', async () => {
    const { finishMihomoApply, confirmStore, toastStore, t } = await load();
    await finishMihomoApply({ ...inactiveRunning, active_running: false });
    expect(get(confirmStore)).toBeNull();
    expect(startMihomo).not.toHaveBeenCalled();
    expect(lastToast(toastStore).message).toBe(get(t)('apply.mihomo_saved_after_switch'));
  });

  it('остальные исходы идут через общий тост без диалога', async () => {
    const { finishMihomoApply, confirmStore, toastStore, t } = await load();
    await finishMihomoApply({ outcome: 'saved_kernel_stopped', kernel: 'mihomo' });
    expect(get(confirmStore)).toBeNull();
    const toast = lastToast(toastStore);
    expect(toast.message).toBe(get(t)('apply.saved_kernel_stopped'));
    expect(toast.action?.label).toBe(get(t)('apply.start_now'));
  });
});
