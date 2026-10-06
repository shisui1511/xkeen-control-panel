import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';

vi.mock('./api', () => ({ apiFetchJSON: vi.fn() }));

import { apiFetchJSON } from './api';
import { toastStore } from '../stores';
import { t, i18nReady } from '../i18n';
import {
  reduceLayerEvent,
  formatBadgeCount,
  handleLayerError,
  startConfigLayer,
  stopConfigLayer,
  refetchLayerState,
  editDraft,
  resetDraft,
  applyNow,
  layerSnapshot,
  layerStatus,
  layerConnection,
  applyStartedHere,
  draftChanges,
  driftCount,
  applyRunning,
  navBadgeCount,
  filesByPath,
  type LayerSnapshot
} from './configLayer';

const apiMock = vi.mocked(apiFetchJSON);

function snapshot(over: Partial<LayerSnapshot> = {}): LayerSnapshot {
  return {
    enabled: true,
    dev_mode: false,
    draft_revision: 1,
    draft_changes: 0,
    drift_count: 0,
    files: [],
    kernels: [],
    features: {},
    apply: { running: false, steps: [] },
    notices: [],
    ...over
  };
}

describe('reduceLayerEvent', () => {
  it('snapshot заменяет состояние целиком', () => {
    const next = reduceLayerEvent(null, 'snapshot', snapshot({ draft_changes: 4 }));
    expect(next?.draft_changes).toBe(4);
    expect(next?.files).toEqual([]);
  });

  it('snapshot с пропущенными массивами получает значения по умолчанию', () => {
    const next = reduceLayerEvent(null, 'snapshot', {
      enabled: true,
      draft_revision: 3,
      draft_changes: 0,
      drift_count: 0,
      files: null,
      apply: { running: false, steps: null }
    });
    expect(next?.files).toEqual([]);
    expect(next?.notices).toEqual([]);
    expect(next?.kernels).toEqual([]);
    expect(next?.apply.steps).toEqual([]);
  });

  it('draft обновляет ревизию и счётчик', () => {
    const next = reduceLayerEvent(snapshot(), 'draft', { draft_revision: 2, draft_changes: 3 });
    expect(next?.draft_revision).toBe(2);
    expect(next?.draft_changes).toBe(3);
  });

  it('draft с устаревшей ревизией не откатывает более новую', () => {
    const state = snapshot({ draft_revision: 9, draft_changes: 4 });
    const next = reduceLayerEvent(state, 'draft', { draft_revision: 8, draft_changes: 1 });
    expect(next).toBe(state);
    expect(next?.draft_revision).toBe(9);
  });

  it('files обновляет список файлов и счётчик расхождений', () => {
    const files = [
      {
        key: 'xray:a',
        kernel: 'xray' as const,
        path: 'xcp-a.json',
        owner: 'panel' as const,
        state: 'drift_modified' as const
      }
    ];
    const next = reduceLayerEvent(snapshot(), 'files', { files, drift_count: 1 });
    expect(next?.files).toEqual(files);
    expect(next?.drift_count).toBe(1);
  });

  it('apply_step запускает применение и ставит шаг', () => {
    const next = reduceLayerEvent(snapshot(), 'apply_step', { id: 'build', state: 'running' });
    expect(next?.apply.running).toBe(true);
    expect(next?.apply.steps.map((s) => s.id)).toEqual([
      'build',
      'validate_xray',
      'validate_mihomo',
      'write',
      'restart'
    ]);
    expect(next?.apply.steps[0].state).toBe('running');
    expect(next?.apply.steps[1].state).toBe('pending');
  });

  it('apply_step внутри идущего применения меняет только свой шаг', () => {
    const running = reduceLayerEvent(snapshot(), 'apply_step', { id: 'build', state: 'running' });
    const next = reduceLayerEvent(running, 'apply_step', { id: 'build', state: 'done' });
    const after = reduceLayerEvent(next, 'apply_step', { id: 'validate_xray', state: 'running' });
    expect(after?.apply.steps[0].state).toBe('done');
    expect(after?.apply.steps[1].state).toBe('running');
    expect(after?.apply.steps).toHaveLength(5);
  });

  it('apply_step после завершённого запуска начинает новый, без старого итога', () => {
    const finished = snapshot({
      apply: {
        running: false,
        steps: [{ id: 'build', state: 'done' }],
        result: { ok: true, code: 'ok', written: 2 }
      }
    });
    const next = reduceLayerEvent(finished, 'apply_step', { id: 'build', state: 'running' });
    expect(next?.apply.running).toBe(true);
    expect(next?.apply.result).toBeUndefined();
    expect(next?.apply.steps).toHaveLength(5);
  });

  it('apply_done заменяет применение итогом', () => {
    const next = reduceLayerEvent(snapshot(), 'apply_done', {
      running: false,
      steps: [{ id: 'build', state: 'failed', message: 'boom' }],
      result: { ok: false, code: 'validation', written: 0 }
    });
    expect(next?.apply.running).toBe(false);
    expect(next?.apply.result?.ok).toBe(false);
    expect(next?.apply.steps[0].state).toBe('failed');
  });

  it('notices заменяет уведомления', () => {
    const next = reduceLayerEvent(snapshot(), 'notices', {
      notices: [{ id: 'schema_reset', kind: 'warning' }]
    });
    expect(next?.notices).toEqual([{ id: 'schema_reset', kind: 'warning' }]);
  });

  it('неизвестное событие не меняет состояние', () => {
    const state = snapshot();
    expect(reduceLayerEvent(state, 'unknown', { a: 1 })).toBe(state);
  });

  it('событие без состояния (кроме snapshot) игнорируется', () => {
    expect(reduceLayerEvent(null, 'draft', { draft_revision: 2, draft_changes: 1 })).toBeNull();
  });

  it('битые данные события не ломают состояние', () => {
    const state = snapshot();
    expect(reduceLayerEvent(state, 'draft', null)).toBe(state);
    expect(reduceLayerEvent(state, 'files', 'x')).toBe(state);
  });
});

describe('formatBadgeCount', () => {
  it('0 и отрицательные — пусто', () => {
    expect(formatBadgeCount(0)).toBe('');
    expect(formatBadgeCount(-1)).toBe('');
  });
  it('1–99 — число', () => {
    expect(formatBadgeCount(1)).toBe('1');
    expect(formatBadgeCount(5)).toBe('5');
    expect(formatBadgeCount(99)).toBe('99');
  });
  it('больше 99 — 99+', () => {
    expect(formatBadgeCount(100)).toBe('99+');
    expect(formatBadgeCount(120)).toBe('99+');
  });
});

describe('производные сторы', () => {
  beforeEach(() => layerSnapshot.set(null));

  it('без состояния — нули', () => {
    expect(get(draftChanges)).toBe(0);
    expect(get(driftCount)).toBe(0);
    expect(get(applyRunning)).toBe(false);
    expect(get(navBadgeCount)).toBe('');
  });

  it('значения берутся из снимка', () => {
    layerSnapshot.set(
      snapshot({
        draft_changes: 120,
        drift_count: 2,
        apply: { running: true, steps: [] },
        files: [
          {
            key: 'xray:a',
            kernel: 'xray',
            path: '/opt/etc/xray/configs/xcp-a.json',
            owner: 'panel',
            state: 'ok'
          }
        ]
      })
    );
    expect(get(draftChanges)).toBe(120);
    expect(get(driftCount)).toBe(2);
    expect(get(applyRunning)).toBe(true);
    expect(get(navBadgeCount)).toBe('99+');
    expect(get(filesByPath).get('/opt/etc/xray/configs/xcp-a.json')?.key).toBe('xray:a');
  });

  it('filesByPath отдаёт файл и по alias-пути симлинка', () => {
    layerSnapshot.set(
      snapshot({
        files: [
          {
            key: 'mihomo:p',
            kernel: 'mihomo',
            path: '/opt/etc/mihomo/profiles/xcp-a.yaml',
            alias_paths: ['/opt/etc/mihomo/config.yaml'],
            owner: 'panel',
            state: 'ok'
          }
        ]
      })
    );
    const map = get(filesByPath);
    expect(map.get('/opt/etc/mihomo/config.yaml')?.key).toBe('mihomo:p');
    expect(map.get('/opt/etc/mihomo/profiles/xcp-a.yaml')?.key).toBe('mihomo:p');
  });
});

describe('API-клиент', () => {
  beforeEach(() => {
    apiMock.mockReset();
    layerSnapshot.set(snapshot({ draft_revision: 7 }));
    applyStartedHere.set(false);
  });

  it('editDraft отправляет текущую ревизию и применяет ответ', async () => {
    apiMock.mockResolvedValueOnce({ draft_revision: 8, draft_changes: 2 });
    await editDraft('nodes', { a: 1 });
    const [url, opts] = apiMock.mock.calls[0];
    expect(url).toBe('/api/configlayer/draft');
    expect(opts?.method).toBe('POST');
    expect(JSON.parse(String(opts?.body))).toEqual({
      revision: 7,
      section: 'nodes',
      value: { a: 1 }
    });
    expect(get(layerSnapshot)?.draft_revision).toBe(8);
    expect(get(layerSnapshot)?.draft_changes).toBe(2);
  });

  it('resetDraft отправляет ревизию', async () => {
    apiMock.mockResolvedValueOnce({ draft_revision: 8, draft_changes: 0 });
    await resetDraft();
    const [url, opts] = apiMock.mock.calls[0];
    expect(url).toBe('/api/configlayer/draft/reset');
    expect(JSON.parse(String(opts?.body))).toEqual({ revision: 7 });
    expect(get(draftChanges)).toBe(0);
  });

  it('applyNow выставляет applyStartedHere; сброс — по apply_done', async () => {
    apiMock.mockResolvedValueOnce({ started: true });
    await applyNow();
    expect(get(applyStartedHere)).toBe(true);
    expect(apiMock.mock.calls[0][0]).toBe('/api/configlayer/apply');
  });

  it('ошибка applyNow не выставляет applyStartedHere', async () => {
    apiMock.mockRejectedValueOnce(Object.assign(new Error('busy'), { code: 'apply_busy' }));
    await expect(applyNow()).rejects.toThrow('busy');
    expect(get(applyStartedHere)).toBe(false);
  });
});

describe('handleLayerError', () => {
  beforeEach(async () => {
    await i18nReady;
    apiMock.mockReset();
    toastStore.set([]);
    layerSnapshot.set(snapshot());
  });

  it('draft_conflict — предупреждение 6000 мс и тихое перечитывание', async () => {
    apiMock.mockResolvedValueOnce(snapshot({ draft_revision: 9, draft_changes: 5 }));
    handleLayerError(Object.assign(new Error('conflict'), { code: 'draft_conflict' }));
    const toasts = get(toastStore);
    expect(toasts).toHaveLength(1);
    expect(toasts[0].type).toBe('warning');
    expect(toasts[0].duration).toBe(6000);
    expect(toasts[0].message).toBe(get(t)('cfg.toast.draft_conflict'));
    expect(toasts[0].message).not.toBe('cfg.toast.draft_conflict');
    await vi.waitFor(() => expect(apiMock).toHaveBeenCalledWith('/api/configlayer/state'));
    await vi.waitFor(() => expect(get(layerSnapshot)?.draft_revision).toBe(9));
  });

  it('apply_busy — информационный тост без перечитывания', () => {
    handleLayerError(Object.assign(new Error('busy'), { code: 'apply_busy' }));
    const toasts = get(toastStore);
    expect(toasts).toHaveLength(1);
    expect(toasts[0].type).toBe('info');
    expect(toasts[0].message).toBe(get(t)('cfg.toast.apply_busy'));
    expect(apiMock).not.toHaveBeenCalled();
  });

  it('file_managed — предупреждение и перечитывание состояния слоя', async () => {
    apiMock.mockResolvedValueOnce(snapshot({ draft_revision: 11 }));
    handleLayerError(Object.assign(new Error('Файл панели'), { code: 'file_managed' }));
    const toasts = get(toastStore);
    expect(toasts).toHaveLength(1);
    expect(toasts[0].type).toBe('warning');
    expect(toasts[0].message).toBe('Файл панели');
    await vi.waitFor(() => expect(apiMock).toHaveBeenCalledWith('/api/configlayer/state'));
  });

  it('прочие ошибки — тост с текстом сервера', () => {
    handleLayerError(new Error('Не вышло'));
    const toasts = get(toastStore);
    expect(toasts[0].type).toBe('error');
    expect(toasts[0].message).toBe('Не вышло');
  });
});

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  readyState = 0;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  listeners = new Map<string, ((e: { data: string }) => void)[]>();
  closed = false;
  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }
  addEventListener(name: string, fn: (e: { data: string }) => void) {
    this.listeners.set(name, [...(this.listeners.get(name) ?? []), fn]);
  }
  close() {
    this.closed = true;
    this.readyState = 2;
  }
  open() {
    this.readyState = 1;
    this.onopen?.();
  }
  emit(name: string, data: unknown) {
    for (const fn of this.listeners.get(name) ?? []) fn({ data: JSON.stringify(data) });
  }
  emitRaw(name: string, data: string) {
    for (const fn of this.listeners.get(name) ?? []) fn({ data });
  }
  fail(readyState: number) {
    this.readyState = readyState;
    this.onerror?.();
  }
}

describe('startConfigLayer / stopConfigLayer', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    FakeEventSource.instances = [];
    vi.stubGlobal('EventSource', FakeEventSource);
    apiMock.mockReset();
    apiMock.mockResolvedValue(snapshot({ draft_revision: 1, draft_changes: 3 }));
    layerSnapshot.set(null);
    layerStatus.set('idle');
    layerConnection.set('connecting');
  });

  afterEach(() => {
    stopConfigLayer();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('загружает состояние и открывает ровно одно соединение', async () => {
    startConfigLayer();
    startConfigLayer();
    expect(get(layerStatus)).toBe('loading');
    await vi.advanceTimersByTimeAsync(0);
    expect(apiMock).toHaveBeenCalledTimes(1);
    expect(apiMock).toHaveBeenCalledWith('/api/configlayer/state');
    expect(get(layerStatus)).toBe('ready');
    expect(get(draftChanges)).toBe(3);
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.instances[0].url).toBe('/api/configlayer/events');
  });

  it('сбой первой загрузки — статус error', async () => {
    apiMock.mockReset();
    apiMock.mockRejectedValue(new Error('boom'));
    startConfigLayer();
    await vi.advanceTimersByTimeAsync(0);
    expect(get(layerStatus)).toBe('error');
  });

  it('события SSE обновляют стор; битый JSON игнорируется', async () => {
    startConfigLayer();
    await vi.advanceTimersByTimeAsync(0);
    const es = FakeEventSource.instances[0];
    es.open();
    expect(get(layerConnection)).toBe('open');
    es.emit('draft', { draft_revision: 2, draft_changes: 6 });
    expect(get(draftChanges)).toBe(6);
    es.emitRaw('draft', '{не json');
    expect(get(draftChanges)).toBe(6);
  });

  it('apply_done сбрасывает applyStartedHere', async () => {
    startConfigLayer();
    await vi.advanceTimersByTimeAsync(0);
    applyStartedHere.set(true);
    FakeEventSource.instances[0].emit('apply_done', {
      running: false,
      steps: [],
      result: { ok: true, code: 'ok', written: 1 }
    });
    expect(get(applyStartedHere)).toBe(false);
  });

  it('обрыв: статус lost, переподключение через 3 с, полный GET после восстановления', async () => {
    startConfigLayer();
    await vi.advanceTimersByTimeAsync(0);
    const first = FakeEventSource.instances[0];
    first.open();
    apiMock.mockClear();
    first.fail(2);
    expect(get(layerConnection)).toBe('lost');
    expect(first.closed).toBe(true);
    await vi.advanceTimersByTimeAsync(2999);
    expect(FakeEventSource.instances).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(FakeEventSource.instances).toHaveLength(2);
    FakeEventSource.instances[1].open();
    await vi.advanceTimersByTimeAsync(0);
    expect(get(layerConnection)).toBe('open');
    expect(apiMock).toHaveBeenCalledWith('/api/configlayer/state');
  });

  it('повторные обрывы удваивают задержку до 30 с', async () => {
    startConfigLayer();
    await vi.advanceTimersByTimeAsync(0);
    FakeEventSource.instances[0].fail(2);
    await vi.advanceTimersByTimeAsync(3000);
    expect(FakeEventSource.instances).toHaveLength(2);
    FakeEventSource.instances[1].fail(2);
    await vi.advanceTimersByTimeAsync(5999);
    expect(FakeEventSource.instances).toHaveLength(2);
    await vi.advanceTimersByTimeAsync(1);
    expect(FakeEventSource.instances).toHaveLength(3);
    for (let i = 0; i < 6; i++) {
      FakeEventSource.instances[FakeEventSource.instances.length - 1].fail(2);
      await vi.advanceTimersByTimeAsync(30000);
    }
    const before = FakeEventSource.instances.length;
    FakeEventSource.instances[before - 1].fail(2);
    await vi.advanceTimersByTimeAsync(29999);
    expect(FakeEventSource.instances).toHaveLength(before);
    await vi.advanceTimersByTimeAsync(1);
    expect(FakeEventSource.instances).toHaveLength(before + 1);
  });

  it('stopConfigLayer закрывает соединение и снимает таймер', async () => {
    startConfigLayer();
    await vi.advanceTimersByTimeAsync(0);
    const es = FakeEventSource.instances[0];
    es.fail(2);
    stopConfigLayer();
    await vi.advanceTimersByTimeAsync(60000);
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(es.closed).toBe(true);
    expect(get(layerSnapshot)).toBeNull();
  });

  it('refetchLayerState перечитывает состояние', async () => {
    apiMock.mockReset();
    apiMock.mockResolvedValue(snapshot({ draft_changes: 11 }));
    await refetchLayerState();
    expect(get(draftChanges)).toBe(11);
    expect(get(layerStatus)).toBe('ready');
  });
});
