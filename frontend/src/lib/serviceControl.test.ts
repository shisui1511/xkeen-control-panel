import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';

// Модули держат состояние на уровне модуля (тосты, окно перезапуска, язык):
// каждый тест заново импортирует всё после vi.resetModules(), чтобы читать
// те же экземпляры сторов, в которые пишет serviceControl (как в api.test.ts).
async function load() {
  const mod = await import('./serviceControl');
  const stores = await import('../stores');
  const i18n = await import('../i18n');
  // Словарь грузится асинхронно: без ожидания в нагруженном прогоне t() вернёт ключ
  await i18n.i18nReady;
  const grace = await import('./serviceGrace');
  return {
    ...mod,
    toastStore: stores.toastStore,
    t: i18n.t,
    isServiceRestarting: grace.isServiceRestarting
  };
}

function makeResponse(status: number, body: unknown): Response {
  return {
    status,
    ok: status >= 200 && status < 300,
    json: () => Promise.resolve(body),
    text: () => Promise.resolve(typeof body === 'string' ? body : JSON.stringify(body)),
    clone: () => makeResponse(status, body)
  } as unknown as Response;
}

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.resetModules();
  vi.useFakeTimers();
  fetchMock = vi.fn();
  vi.stubGlobal('fetch', fetchMock);
  vi.stubGlobal('localStorage', {
    getItem: vi.fn().mockImplementation((k: string) => (k === 'csrf_token' ? 'csrf' : 'ru')),
    setItem: vi.fn(),
    removeItem: vi.fn()
  });
  vi.stubGlobal('window', { location: { hash: '' } });
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('stopKernelProcess', () => {
  it('stopped: POST action=stop&kernel с CSRF и результат сервера', async () => {
    fetchMock.mockResolvedValue(
      makeResponse(200, {
        success: true,
        data: { kernel: 'mihomo', outcome: 'stopped', method: 'signal' }
      })
    );
    const { stopKernelProcess } = await load();

    const res = await stopKernelProcess('mihomo');

    expect(res).toEqual({ kernel: 'mihomo', outcome: 'stopped', method: 'signal' });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/service/control?action=stop&kernel=mihomo');
    expect(init.method).toBe('POST');
    expect((init.headers as Headers).get('X-CSRF-Token')).toBe('csrf');
  });

  it('still_running возвращается как исход, а не ошибка', async () => {
    fetchMock.mockResolvedValue(
      makeResponse(200, {
        success: true,
        data: { kernel: 'xray', outcome: 'still_running', method: 'xkeen' }
      })
    );
    const { stopKernelProcess } = await load();

    const res = await stopKernelProcess('xray');

    expect(res.outcome).toBe('still_running');
  });

  it('неизвестный outcome: ошибка Invalid stop response', async () => {
    fetchMock.mockResolvedValue(
      makeResponse(200, { success: true, data: { kernel: 'xray', outcome: 'weird' } })
    );
    const { stopKernelProcess } = await load();

    await expect(stopKernelProcess('xray')).rejects.toThrow('Invalid stop response');
  });

  it('ответ без data: ошибка Invalid stop response', async () => {
    fetchMock.mockResolvedValue(makeResponse(200, { success: true }));
    const { stopKernelProcess } = await load();

    await expect(stopKernelProcess('xray')).rejects.toThrow('Invalid stop response');
  });

  it('409 kernel_op_in_progress: текст kernel.op_in_progress', async () => {
    fetchMock.mockResolvedValue(
      makeResponse(409, { success: false, error: 'busy', code: 'kernel_op_in_progress' })
    );
    const { stopKernelProcess, t } = await load();

    await expect(stopKernelProcess('mihomo')).rejects.toMatchObject({
      message: get(t)('kernel.op_in_progress'),
      status: 409,
      code: 'kernel_op_in_progress'
    });
  });
});

describe('serviceAction', () => {
  it('успех: текст ответа', async () => {
    fetchMock.mockResolvedValue(makeResponse(200, 'Service started'));
    const { serviceAction } = await load();

    await expect(serviceAction('start')).resolves.toBe('Service started');
    expect(fetchMock.mock.calls[0][0]).toBe('/api/service/control?action=start');
  });

  it('409 kernel_conflict: переведённый текст, а не сырое тело', async () => {
    fetchMock.mockResolvedValue(makeResponse(409, { code: 'kernel_conflict', error: 'both' }));
    const { serviceAction, t } = await load();

    await expect(serviceAction('restart')).rejects.toMatchObject({
      message: get(t)('kernel.conflict_blocked_toast'),
      status: 409,
      code: 'kernel_conflict'
    });
  });

  it('409 kernel_conflict на restart: окно перезапуска снято', async () => {
    fetchMock.mockResolvedValue(makeResponse(409, { code: 'kernel_conflict', error: 'both' }));
    const { serviceAction, isServiceRestarting } = await load();

    await expect(serviceAction('restart')).rejects.toMatchObject({ code: 'kernel_conflict' });
    expect(get(isServiceRestarting)).toBe(false);
  });

  it('409 kernel_op_in_progress на restart: окно чужой операции не тронуто', async () => {
    fetchMock.mockResolvedValue(
      makeResponse(409, { success: false, error: 'busy', code: 'kernel_op_in_progress' })
    );
    const { serviceAction, isServiceRestarting } = await load();

    await expect(serviceAction('restart')).rejects.toMatchObject({
      code: 'kernel_op_in_progress'
    });
    expect(get(isServiceRestarting)).toBe(true);
  });

  it('ошибка stop окно не трогает', async () => {
    fetchMock.mockResolvedValue(makeResponse(500, 'boom'));
    const { serviceAction, isServiceRestarting } = await load();
    const { activateRestartGrace } = await import('./serviceGrace');
    activateRestartGrace(6000);

    await expect(serviceAction('stop')).rejects.toThrow('boom');
    expect(get(isServiceRestarting)).toBe(true);
  });

  it('500 с текстом: текст ответа', async () => {
    fetchMock.mockResolvedValue(makeResponse(500, 'xkeen exploded'));
    const { serviceAction } = await load();

    await expect(serviceAction('start')).rejects.toMatchObject({
      message: 'xkeen exploded',
      status: 500
    });
  });

  it('500 с конвертом: поле error', async () => {
    fetchMock.mockResolvedValue(makeResponse(500, { success: false, error: 'from envelope' }));
    const { serviceAction } = await load();

    await expect(serviceAction('start')).rejects.toThrow('from envelope');
  });

  it('пустой ответ ошибки: HTTP <status>', async () => {
    fetchMock.mockResolvedValue(makeResponse(502, ''));
    const { serviceAction } = await load();

    await expect(serviceAction('stop')).rejects.toThrow('HTTP 502');
  });

  it('restart включает окно перезапуска, stop нет', async () => {
    fetchMock.mockResolvedValue(makeResponse(200, 'ok'));
    const { serviceAction, isServiceRestarting } = await load();

    await serviceAction('stop');
    expect(get(isServiceRestarting)).toBe(false);

    await serviceAction('restart');
    expect(get(isServiceRestarting)).toBe(true);
  });
});

describe('switchKernel', () => {
  const switched = {
    outcome: 'switched',
    old: 'xray',
    new: 'mihomo',
    old_running: false,
    new_running: true,
    output: ''
  };

  it('switched на Mihomo: окно включено до запроса, затем окно прогрева API 6 с', async () => {
    let graceDuringRequest = false;
    const { switchKernel, isServiceRestarting } = await load();
    fetchMock.mockImplementation(async () => {
      graceDuringRequest = get(isServiceRestarting);
      return makeResponse(200, { success: true, data: switched });
    });

    const res = await switchKernel('mihomo');

    expect(res).toMatchObject({
      outcome: 'switched',
      old: 'xray',
      new: 'mihomo',
      new_running: true
    });
    expect(graceDuringRequest).toBe(true);
    expect(fetchMock.mock.calls[0][0]).toBe(
      '/api/service/control?action=switch_kernel&kernel=mihomo'
    );
    // 20-секундное окно заменено прогревом API: через 5 с активно, через 7 с снято
    expect(get(isServiceRestarting)).toBe(true);
    vi.advanceTimersByTime(5000);
    expect(get(isServiceRestarting)).toBe(true);
    vi.advanceTimersByTime(2000);
    expect(get(isServiceRestarting)).toBe(false);
  });

  it('switched на Xray: окно снимается сразу', async () => {
    fetchMock.mockResolvedValue(
      makeResponse(200, {
        success: true,
        data: { ...switched, old: 'mihomo', new: 'xray' }
      })
    );
    const { switchKernel, isServiceRestarting } = await load();

    const res = await switchKernel('xray');

    expect(res.outcome).toBe('switched');
    expect(get(isServiceRestarting)).toBe(false);
  });

  it('old_still_running и new_not_started: окно снимается сразу', async () => {
    const { switchKernel, isServiceRestarting } = await load();
    for (const outcome of ['old_still_running', 'new_not_started']) {
      fetchMock.mockResolvedValue(
        makeResponse(200, { success: true, data: { ...switched, outcome } })
      );
      const res = await switchKernel('mihomo');
      expect(res.outcome).toBe(outcome);
      expect(get(isServiceRestarting)).toBe(false);
    }
  });

  it('неизвестный outcome: ошибка Invalid switch response, окно снято', async () => {
    fetchMock.mockResolvedValue(
      makeResponse(200, { success: true, data: { ...switched, outcome: 'weird' } })
    );
    const { switchKernel, isServiceRestarting } = await load();

    await expect(switchKernel('mihomo')).rejects.toThrow('Invalid switch response');
    expect(get(isServiceRestarting)).toBe(false);
  });

  it('ответ без data: ошибка Invalid switch response', async () => {
    fetchMock.mockResolvedValue(makeResponse(200, { success: true }));
    const { switchKernel } = await load();

    await expect(switchKernel('mihomo')).rejects.toThrow('Invalid switch response');
  });

  it('409 kernel_conflict: переведённый текст, окно снято', async () => {
    fetchMock.mockResolvedValue(
      makeResponse(409, { success: false, error: 'both', code: 'kernel_conflict' })
    );
    const { switchKernel, isServiceRestarting, t } = await load();

    await expect(switchKernel('mihomo')).rejects.toMatchObject({
      message: get(t)('kernel.conflict_blocked_toast'),
      code: 'kernel_conflict'
    });
    expect(get(isServiceRestarting)).toBe(false);
  });

  it('409 kernel_op_in_progress: окно чужой операции не снимается', async () => {
    fetchMock.mockResolvedValue(
      makeResponse(409, { success: false, error: 'busy', code: 'kernel_op_in_progress' })
    );
    const { switchKernel, isServiceRestarting } = await load();

    await expect(switchKernel('mihomo')).rejects.toMatchObject({
      code: 'kernel_op_in_progress'
    });
    expect(get(isServiceRestarting)).toBe(true);
  });

  it('500 с выводом скрипта: текст ошибки из конверта', async () => {
    fetchMock.mockResolvedValue(makeResponse(500, { success: false, error: 'script failed' }));
    const { switchKernel } = await load();

    await expect(switchKernel('xray')).rejects.toThrow('script failed');
  });
});

describe('notifySwitchOutcome', () => {
  const base = {
    old: 'xray',
    new: 'mihomo',
    old_running: false,
    new_running: true,
    output: ''
  } as const;

  it('switched: success 4000', async () => {
    const { notifySwitchOutcome, toastStore } = await load();
    notifySwitchOutcome({ ...base, outcome: 'switched' });
    const [toast] = get(toastStore);
    expect(toast.type).toBe('success');
    expect(toast.duration).toBe(4000);
    expect(toast.message).toBe('Ядро переключено на Mihomo');
  });

  it('old_still_running: warning 10000 с именем прежнего ядра', async () => {
    const { notifySwitchOutcome, toastStore } = await load();
    notifySwitchOutcome({ ...base, outcome: 'old_still_running', old_running: true });
    const [toast] = get(toastStore);
    expect(toast.type).toBe('warning');
    expect(toast.duration).toBe(10000);
    expect(toast.message).toContain('Xray всё ещё работает');
  });

  it('new_not_started: error 10000 с действием «Логи»', async () => {
    const { notifySwitchOutcome, toastStore, t } = await load();
    notifySwitchOutcome({ ...base, outcome: 'new_not_started', new_running: false });
    const [toast] = get(toastStore);
    expect(toast.type).toBe('error');
    expect(toast.duration).toBe(10000);
    expect(toast.message).toContain('Ядро Mihomo не запустилось, Xray остановлен');
    expect(toast.action?.label).toBe(get(t)('apply.open_logs'));
    toast.action?.onClick();
    expect((window as unknown as { location: { hash: string } }).location.hash).toBe('#/logs');
  });

  it('new_not_started при old=none: ключ _plain без прежнего ядра', async () => {
    const { notifySwitchOutcome, toastStore, t } = await load();
    notifySwitchOutcome({ ...base, old: 'none', outcome: 'new_not_started', new_running: false });
    const [toast] = get(toastStore);
    expect(toast.message).toBe(get(t)('kernel.switch_new_not_started_plain', { new: 'Mihomo' }));
  });
});
