import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';

// Модуль тянет ../stores и ../i18n, у которых есть состояние на уровне модуля:
// чтобы читать тот же toastStore, в который пишет serviceApply, каждый тест
// заново импортирует всё после vi.resetModules() (как в api.test.ts).
const apiFetchJSON = vi.fn();
const apiFetch = vi.fn();

const switchKernel = vi.fn();
const serviceAction = vi.fn();

async function load() {
  vi.doMock('./api', () => ({ apiFetch, apiFetchJSON }));
  // switchKernel и serviceAction подменены; тосты исхода — настоящие
  vi.doMock('./serviceControl', async () => {
    const actual = await vi.importActual<typeof import('./serviceControl')>('./serviceControl');
    return { ...actual, switchKernel, serviceAction };
  });
  const mod = await import('./serviceApply');
  const stores = await import('../stores');
  const i18n = await import('../i18n');
  // Словарь грузится асинхронно: без ожидания в нагруженном прогоне t() вернёт ключ
  await i18n.i18nReady;
  const grace = await import('./serviceGrace');
  const mihomo = await import('./constructors/mihomoApply');
  return {
    ...mod,
    isServiceRestarting: grace.isServiceRestarting,
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
  switchKernel.mockReset();
  serviceAction.mockReset();
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
  vi.doUnmock('./serviceControl');
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

  it('saved_kernel_conflict: error без действия и без обещания перезапуска', async () => {
    const { notifyApplyOutcome, toastStore, t } = await load();
    notifyApplyOutcome({ outcome: 'saved_kernel_conflict', kernel: 'xray' });
    const [toast] = get(toastStore);
    expect(toast.type).toBe('error');
    expect(toast.message).toBe(get(t)('apply.saved_kernel_conflict'));
    expect(toast.action).toBeUndefined();
  });

  it('saved_op_in_progress: warning без действия и без «сбоя рестарта»', async () => {
    const { notifyApplyOutcome, toastStore, t } = await load();
    notifyApplyOutcome({ outcome: 'saved_op_in_progress', kernel: 'xray' });
    const [toast] = get(toastStore);
    expect(toast.type).toBe('warning');
    expect(toast.message).toBe(get(t)('apply.saved_op_in_progress'));
    expect(toast.message).toContain('идёт другая операция с ядром');
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

  it('saved_kernel_conflict распознаётся как известный исход', async () => {
    apiFetchJSON.mockResolvedValue({ outcome: 'saved_kernel_conflict', kernel: 'xray' });
    const { applyToKernel } = await load();
    expect((await applyToKernel({ kernel: 'xray' })).outcome).toBe('saved_kernel_conflict');
  });

  it('неизвестный или отсутствующий outcome даёт unknown', async () => {
    const { applyToKernel } = await load();
    apiFetchJSON.mockResolvedValueOnce({ outcome: 'exploded', kernel: 'xray' });
    expect((await applyToKernel({ kernel: 'xray' })).outcome).toBe('unknown');
    apiFetchJSON.mockResolvedValueOnce(null);
    expect((await applyToKernel({ kernel: 'xray' })).outcome).toBe('unknown');
  });

  it('ошибка HTTP пробрасывается вызывающему, окно перезапуска снято', async () => {
    apiFetchJSON.mockRejectedValue(new Error('HTTP 500'));
    const { applyToKernel, isServiceRestarting } = await load();
    await expect(applyToKernel({ kernel: 'xray' })).rejects.toThrow('HTTP 500');
    expect(get(isServiceRestarting)).toBe(false);
  });

  function gateError(code: string, status = 409) {
    return Object.assign(new Error(code), { status, code });
  }

  it('409 kernel_conflict: исход saved_kernel_conflict, окно снято', async () => {
    apiFetchJSON.mockRejectedValue(gateError('kernel_conflict'));
    const { applyToKernel, isServiceRestarting } = await load();
    const result = await applyToKernel({ kernel: 'xray' });
    expect(result).toMatchObject({
      outcome: 'saved_kernel_conflict',
      kernel: 'xray',
      active_kernel: 'both',
      active_running: true
    });
    expect(get(isServiceRestarting)).toBe(false);
  });

  it('409 kernel_conflict для active и path: имя ядра пустое', async () => {
    apiFetchJSON.mockRejectedValue(gateError('kernel_conflict'));
    const { applyToKernel } = await load();
    expect((await applyToKernel({ kernel: 'active' })).kernel).toBe('');
    expect((await applyToKernel({ path: '/opt/etc/xray/a.json' })).kernel).toBe('');
  });

  it('409 kernel_op_in_progress: исход saved_op_in_progress, окно не снимается', async () => {
    apiFetchJSON.mockRejectedValue(gateError('kernel_op_in_progress'));
    const { applyToKernel, isServiceRestarting } = await load();
    const result = await applyToKernel({ kernel: 'mihomo' });
    expect(result).toMatchObject({ outcome: 'saved_op_in_progress', kernel: 'mihomo' });
    expect(get(isServiceRestarting)).toBe(true);
  });

  it('409 с другим кодом (kernel_inactive) пробрасывается, окно снято', async () => {
    apiFetchJSON.mockRejectedValue(gateError('kernel_inactive'));
    const { applyToKernel, isServiceRestarting } = await load();
    await expect(applyToKernel({ kernel: 'xray' })).rejects.toMatchObject({
      code: 'kernel_inactive'
    });
    expect(get(isServiceRestarting)).toBe(false);
  });

  it('код kernel_conflict не со статусом 409 не превращается в исход', async () => {
    apiFetchJSON.mockRejectedValue(gateError('kernel_conflict', 500));
    const { applyToKernel } = await load();
    await expect(applyToKernel({ kernel: 'xray' })).rejects.toThrow();
  });

  it('исход без рестарта снимает окно, restarted оставляет', async () => {
    const { applyToKernel, isServiceRestarting } = await load();
    apiFetchJSON.mockResolvedValueOnce({ outcome: 'saved_kernel_stopped', kernel: 'xray' });
    await applyToKernel({ kernel: 'xray' });
    expect(get(isServiceRestarting)).toBe(false);
    apiFetchJSON.mockResolvedValueOnce({ outcome: 'restarted', kernel: 'xray' });
    await applyToKernel({ kernel: 'xray' });
    expect(get(isServiceRestarting)).toBe(true);
  });
});

describe('startKernelNow', () => {
  it('запускает ядро через serviceAction и обновляет capabilities', async () => {
    serviceAction.mockResolvedValue('ok');
    const { startKernelNow } = await load();
    await startKernelNow();
    expect(serviceAction).toHaveBeenCalledWith('start');
    expect(apiFetchJSON).toHaveBeenCalledWith('/api/capabilities', expect.anything());
  });

  it('ошибка запуска: error-тост с текстом', async () => {
    serviceAction.mockRejectedValue(new Error('no binary'));
    const { startKernelNow, toastStore, t } = await load();
    await startKernelNow();
    expect(get(toastStore)[0].message).toBe(get(t)('apply.start_failed', { error: 'no binary' }));
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

  const switchedResult = {
    outcome: 'switched' as const,
    old: 'xray',
    new: 'mihomo' as const,
    old_running: false,
    new_running: true,
    output: ''
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
    expect(switchKernel).not.toHaveBeenCalled();
    expect(apiFetch).not.toHaveBeenCalled();
    const toast = lastToast(toastStore);
    expect(toast.message).toBe(get(t)('apply.mihomo_saved_after_switch'));
    expect(toast.action).toBeUndefined();
  });

  it('согласие: ровно один switch_kernel и тост успеха', async () => {
    switchKernel.mockResolvedValue(switchedResult);
    const { finishMihomoApply, confirmStore, toastStore } = await load();
    const done = finishMihomoApply(inactiveRunning, 'Готово');
    await answer(confirmStore, true);
    await done;
    expect(switchKernel).toHaveBeenCalledTimes(1);
    expect(switchKernel).toHaveBeenCalledWith('mihomo');
    const toast = lastToast(toastStore);
    expect(toast.type).toBe('success');
    expect(toast.message).toBe('Готово');
  });

  it('исход old_still_running: тост исхода вместо успеха применения', async () => {
    switchKernel.mockResolvedValue({
      ...switchedResult,
      outcome: 'old_still_running',
      old_running: true
    });
    const { finishMihomoApply, confirmStore, toastStore, t } = await load();
    const done = finishMihomoApply(inactiveRunning, 'Готово');
    await answer(confirmStore, true);
    await done;
    const toast = lastToast(toastStore);
    expect(toast.type).toBe('warning');
    expect(toast.message).toBe(get(t)('kernel.switch_old_running', { old: 'Xray' }));
    expect(switchKernel).toHaveBeenCalledTimes(1);
  });

  it('ошибка переключения: тост apply.switch_failed с причиной', async () => {
    switchKernel.mockRejectedValue(new Error('boom'));
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
    expect(switchKernel).not.toHaveBeenCalled();
    expect(lastToast(toastStore).message).toBe(get(t)('apply.mihomo_saved_after_switch'));
  });

  it('нормализованный отказ конфликта: тост «не применено», не сбой рестарта', async () => {
    apiFetchJSON.mockRejectedValueOnce(
      Object.assign(new Error('both'), { status: 409, code: 'kernel_conflict' })
    );
    const { applyToKernel, finishMihomoApply, toastStore, t } = await load();
    const result = await applyToKernel({ kernel: 'mihomo' });
    await finishMihomoApply(result);
    const toast = lastToast(toastStore);
    expect(toast.message).toBe(get(t)('apply.saved_kernel_conflict'));
    expect(toast.message).not.toContain(get(t)('apply.open_logs'));
    expect(toast.action).toBeUndefined();
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
