import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';

// Модули держат состояние на уровне модуля (тосты, окно перезапуска, язык):
// каждый тест заново импортирует всё после vi.resetModules(), чтобы читать
// те же экземпляры сторов, в которые пишет serviceControl (как в api.test.ts).
async function load() {
  const mod = await import('./serviceControl');
  const stores = await import('../stores');
  const i18n = await import('../i18n');
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
