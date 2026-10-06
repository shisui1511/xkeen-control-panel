import type { Page, Route } from '@playwright/test';
import { setupMocks, type KernelMode } from './api-mocks';

// ============================================================
// Моки слоя «Конфигурация»: /api/settings (флаг config_layer) и
// /api/configlayer/*. SSE подменяется управляемым EventSource
// (addInitScript): тест сам шлёт события и рвёт соединение, поэтому
// поведение не зависит от того, как браузер переподключает конечный
// поток. Состояние (снимок, флаг) изменяемо; все запросы пишутся в calls.
// ============================================================

export interface LayerSnapshotMock {
  enabled: boolean;
  dev_mode: boolean;
  draft_revision: number;
  draft_changes: number;
  drift_count: number;
  files: Record<string, unknown>[];
  kernels: Record<string, unknown>[];
  features: Record<string, string>;
  apply: Record<string, unknown>;
  notices: Record<string, unknown>[];
}

export interface MockCall {
  method: string;
  path: string;
  body: unknown;
}

export interface ConfigLayerMockOptions {
  /** Флаг config_layer в GET /api/settings (по умолчанию true). */
  flag?: boolean;
  devMode?: boolean;
  kernel?: KernelMode;
  /** Поверх снимка по умолчанию. */
  snapshot?: Partial<LayerSnapshotMock>;
  /** GET /api/configlayer/state отвечает 500. */
  failState?: boolean;
  /** POST draft и draft/reset отвечают 409 draft_conflict. */
  draftConflict?: boolean;
  /** POST apply отвечает 409 apply_busy. */
  applyBusy?: boolean;
  /** POST /api/settings/config-layer отвечает ошибкой (статус и код). */
  settingsError?: { status: number; code?: string; message?: string };
  /** Ответы GET /files/diff по ключу файла (иначе — простой diff двух версий). */
  diffs?: Record<string, MockDiff>;
  /** Задержка ответа POST /api/settings/config-layer, мс. */
  settingsDelayMs?: number;
}

export interface MockDiff {
  expected: string;
  actual: string;
  missing?: boolean;
  truncated?: boolean;
}

export interface ConfigLayerMock {
  calls: MockCall[];
  diffs: Record<string, MockDiff>;
  snapshot: LayerSnapshotMock;
  /** Текущее значение флага на «сервере». */
  flag: boolean;
  failState: boolean;
  draftConflict: boolean;
  applyBusy: boolean;
  settingsError?: { status: number; code?: string; message?: string };
  /** Запросы по методу и подстроке пути. */
  callsTo(method: string, pathPart: string): MockCall[];
  /** Отправить событие SSE во все открытые соединения слоя. */
  emit(name: string, data: unknown): Promise<void>;
  /** Оборвать соединение; новые попытки падают, пока не вызван reopen. */
  dropConnection(): Promise<void>;
  reopen(): Promise<void>;
  /** Сколько EventSource к /api/configlayer/events создала страница. */
  sseCreated(): Promise<number>;
  /** Дождаться открытого соединения SSE. */
  waitConnected(): Promise<void>;
}

export function defaultSnapshot(over: Partial<LayerSnapshotMock> = {}): LayerSnapshotMock {
  return {
    enabled: true,
    dev_mode: false,
    draft_revision: 1,
    draft_changes: 0,
    drift_count: 0,
    files: [],
    kernels: [
      { name: 'xkeen', installed: true, version: '1.2.0', status: 'ok', min_version: '' },
      { name: 'xray', installed: true, version: '1.8.4', status: 'ok', min_version: '1.8.0' },
      { name: 'mihomo', installed: true, version: '1.18.0', status: 'ok', min_version: '1.18.0' }
    ],
    features: {},
    apply: { running: false, steps: [] },
    notices: [],
    ...over
  };
}

const json = (data: unknown, status = 200) => ({
  status,
  contentType: 'application/json',
  body: JSON.stringify(data)
});

const ok = (data: unknown) => json({ success: true, data });

const fail = (status: number, code: string, error: string) =>
  json({ success: false, error, code }, status);

/**
 * Подменяет EventSource для /api/configlayer/events управляемой заглушкой
 * window.__cfgSse: emit(name, data), drop(), reopen(), created.
 */
async function installFakeEventSource(page: Page): Promise<void> {
  await page.addInitScript(() => {
    type Listener = (e: { data: string }) => void;
    interface Fake {
      url: string;
      readyState: number;
      onopen: (() => void) | null;
      onerror: (() => void) | null;
      listeners: Record<string, Listener[]>;
    }
    const instances: Fake[] = [];
    const ctl = { blocked: false, created: 0 };
    const RealEventSource = window.EventSource;

    class FakeEventSource {
      url: string;
      readyState = 0;
      onopen: (() => void) | null = null;
      onerror: (() => void) | null = null;
      listeners: Record<string, Listener[]> = {};
      constructor(url: string) {
        this.url = url;
        instances.push(this as unknown as Fake);
        ctl.created += 1;
        setTimeout(() => {
          if (this.readyState === 2) return;
          if (ctl.blocked) {
            this.readyState = 2;
            this.onerror?.();
            return;
          }
          this.readyState = 1;
          this.onopen?.();
        }, 0);
      }
      addEventListener(name: string, fn: Listener) {
        (this.listeners[name] ??= []).push(fn);
      }
      removeEventListener() {}
      close() {
        this.readyState = 2;
      }
    }

    function isLayerUrl(u: unknown): boolean {
      return typeof u === 'string' && u.includes('/api/configlayer/events');
    }

    // Прочие EventSource (обновления панели и т.п.) остаются настоящими
    const Patched = function (url: string | URL, init?: EventSourceInit) {
      if (isLayerUrl(String(url))) return new FakeEventSource(String(url));
      return new RealEventSource(url, init);
    } as unknown as typeof EventSource;
    Object.assign(Patched, { CONNECTING: 0, OPEN: 1, CLOSED: 2 });
    window.EventSource = Patched;

    (window as unknown as Record<string, unknown>).__cfgSse = {
      emit(name: string, data: unknown) {
        const payload = JSON.stringify(data);
        for (const es of instances) {
          if (es.readyState !== 1) continue;
          for (const fn of es.listeners[name] ?? []) fn({ data: payload });
        }
      },
      drop() {
        ctl.blocked = true;
        for (const es of instances) {
          if (es.readyState === 2) continue;
          es.readyState = 2;
          es.onerror?.();
        }
      },
      reopen() {
        ctl.blocked = false;
      },
      created: () => ctl.created,
      opened: () => instances.filter((es) => es.readyState === 1).length
    };
  });
}

export async function mockConfigLayer(
  page: Page,
  opts: ConfigLayerMockOptions = {}
): Promise<ConfigLayerMock> {
  await setupMocks(page, opts.kernel ?? 'mihomo');
  await installFakeEventSource(page);

  const mock: ConfigLayerMock = {
    calls: [],
    diffs: opts.diffs ?? {},
    snapshot: defaultSnapshot({ dev_mode: opts.devMode ?? false, ...opts.snapshot }),
    flag: opts.flag ?? true,
    failState: opts.failState ?? false,
    draftConflict: opts.draftConflict ?? false,
    applyBusy: opts.applyBusy ?? false,
    settingsError: opts.settingsError,
    callsTo(method, pathPart) {
      return this.calls.filter((c) => c.method === method && c.path.includes(pathPart));
    },
    async emit(name, data) {
      await page.evaluate(
        ([n, d]) =>
          (window as unknown as { __cfgSse: { emit(n: string, d: unknown): void } }).__cfgSse.emit(
            n as string,
            d
          ),
        [name, data] as [string, unknown]
      );
    },
    async dropConnection() {
      await page.evaluate(() =>
        (window as unknown as { __cfgSse: { drop(): void } }).__cfgSse.drop()
      );
    },
    async reopen() {
      await page.evaluate(() =>
        (window as unknown as { __cfgSse: { reopen(): void } }).__cfgSse.reopen()
      );
    },
    async waitConnected() {
      await page.waitForFunction(
        () =>
          ((window as unknown as { __cfgSse?: { opened(): number } }).__cfgSse?.opened() ?? 0) > 0
      );
    },
    async sseCreated() {
      return page.evaluate(() =>
        (window as unknown as { __cfgSse: { created(): number } }).__cfgSse.created()
      );
    }
  };

  const record = (route: Route): MockCall => {
    const req = route.request();
    let body: unknown = null;
    const raw = req.postData();
    if (raw) {
      try {
        body = JSON.parse(raw);
      } catch {
        body = raw;
      }
    }
    const call = { method: req.method(), path: new URL(req.url()).pathname, body };
    mock.calls.push(call);
    return call;
  };

  // GET /api/settings и POST /api/settings/config-layer
  await page.route('**/api/settings**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/settings/config-layer') {
      const call = record(route);
      if (opts.settingsDelayMs) await new Promise((r) => setTimeout(r, opts.settingsDelayMs));
      if (mock.settingsError) {
        const e = mock.settingsError;
        await route.fulfill(fail(e.status, e.code ?? 'internal', e.message ?? 'Ошибка сервера'));
        return;
      }
      const enabled = Boolean((call.body as { enabled?: boolean } | null)?.enabled);
      mock.flag = enabled;
      mock.snapshot.enabled = enabled;
      await route.fulfill(ok({ config_layer: enabled }));
      return;
    }
    if (path === '/api/settings') {
      await route.fulfill(ok({ dev_mode: opts.devMode ?? false, config_layer: mock.flag }));
      return;
    }
    await route.fallback();
  });

  await page.route('**/api/configlayer/**', async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname.replace('/api/configlayer', '');
    const call = record(route);

    if (!mock.flag) {
      await route.fulfill(fail(404, 'config_layer_disabled', 'Слой «Конфигурация» выключен'));
      return;
    }

    if (path === '/state') {
      if (mock.failState) {
        await route.fulfill(fail(500, 'internal', 'Внутренняя ошибка'));
        return;
      }
      await route.fulfill(ok(mock.snapshot));
      return;
    }
    if (path === '/events') {
      // Настоящий поток подменён заглушкой EventSource; на случай прямого обращения
      await route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' });
      return;
    }
    if (path === '/draft' || path === '/draft/reset' || path === '/diag') {
      if (mock.draftConflict) {
        await route.fulfill(fail(409, 'draft_conflict', 'Черновик изменён в другой вкладке'));
        return;
      }
      mock.snapshot.draft_revision += 1;
      if (path === '/draft/reset') mock.snapshot.draft_changes = 0;
      else mock.snapshot.draft_changes += 1;
      await route.fulfill(
        ok({
          draft_revision: mock.snapshot.draft_revision,
          draft_changes: mock.snapshot.draft_changes
        })
      );
      return;
    }
    if (path === '/apply') {
      if (mock.applyBusy) {
        await route.fulfill(fail(409, 'apply_busy', 'Применение уже идёт'));
        return;
      }
      await route.fulfill(ok({ started: true }));
      return;
    }
    if (path === '/files/rebuild') {
      await route.fulfill(ok({ started: true }));
      return;
    }
    if (path === '/files/release') {
      // Как сервер: файл становится ручным, расхождений становится меньше
      const key = (call.body as { key?: string } | null)?.key;
      for (const f of mock.snapshot.files) {
        if (f.key === key) {
          f.owner = 'manual';
          f.state = 'released';
        }
      }
      mock.snapshot.drift_count = mock.snapshot.files.filter((f) =>
        String(f.state).startsWith('drift_')
      ).length;
      await route.fulfill(
        ok({ files: mock.snapshot.files, drift_count: mock.snapshot.drift_count })
      );
      return;
    }
    if (path === '/files/diff') {
      const key = url.searchParams.get('key') ?? '';
      const d = mock.diffs[key] ?? {
        expected: '{\n  "a": 1\n}\n',
        actual: '{\n  "a": 2\n}\n'
      };
      await route.fulfill(ok({ key, missing: false, truncated: false, ...d }));
      return;
    }
    if (path === '/notices/dismiss') {
      const id = (call.body as { id?: string } | null)?.id;
      mock.snapshot.notices = mock.snapshot.notices.filter((n) => n.id !== id);
      await route.fulfill(ok({ notices: mock.snapshot.notices }));
      return;
    }
    await route.fulfill(fail(404, 'not_found', 'Не найдено'));
  });

  return mock;
}
