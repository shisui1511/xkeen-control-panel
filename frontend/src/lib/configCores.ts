// Клиент секций ядер (вкладка «Xray │ Mihomo» раздела «Конфигурация»): типы ответов API
// /api/configlayer/cores*, таблица полей форм и функции запросов. Поля — snake_case, как
// у сервера. Ключи интерфейсов совпадают с frontend/src/lib/coresContract.json: сверку
// выполняет scripts/check-config-ui.js (необязательные поля — `?`). Интерфейс ничего не
// считает сам: владелец секции, закреплённые значения и проблемы приходят готовыми.

import { get } from 'svelte/store';
import { apiFetchJSON } from './api';
import { onLayerEvent, layerSnapshot } from './configLayer';

// --- Типы контракта ---

export type CoreKernel = 'xray' | 'mihomo';
export type XraySectionId = 'log' | 'dns' | 'inbounds' | 'policy' | 'other';
export type MihomoSectionId = 'main' | 'dns' | 'inbounds' | 'sniffer' | 'other';
/** Кто ведёт секцию: панель, ждёт «Применить», XKeen, пользователь, файла нет, дрейф. */
export type SectionOwner = 'panel' | 'pending' | 'xkeen' | 'user' | 'none' | 'drift';

/** Значение, которое панель дописывает в секцию сама и которое менять нельзя. */
export interface PinnedValue {
  key: string;
  value: unknown;
}

/** Проблема ключа сырого блока: ключ и код (текст — cfg.sec.issue.<код> или cfg.sec.err.<код>). */
export interface KeyIssue {
  key: string;
  code: string;
}

export interface SectionView {
  id: XraySectionId | MihomoSectionId;
  owner: SectionOwner;
  files: string[];
  /** Значение секции целиком (действующее у чужой секции). */
  value: unknown;
  /** Только поля формы секции (FORM_KEYS). */
  form: unknown;
  /** Остаток вне формы: JSON у Xray, YAML у Mihomo. */
  advanced: string;
  pinned: PinnedValue[];
  dirty: boolean;
  panel_file?: string;
  /** Код блокировки взятия (inbounds_mixed, section_shadowed, xkeen_too_old). */
  blocked?: string;
  blocked_file?: string;
  comments?: string[];
  left?: string[];
  drift?: string[];
}

/** Группа, в которую панель направляет входящие force-proxy XKeen. */
export interface ForceProxyView {
  tags: string[];
  group: string;
  editable: boolean;
}

/** Активный профиль Mihomo: кто им управляет и как он называется. */
export interface ProfileView {
  owner: string;
  name: string;
}

export interface KernelView {
  kernel: CoreKernel;
  installed: boolean;
  running: boolean;
  sections: SectionView[];
  take_all: boolean;
  profile?: ProfileView;
  force_proxy?: ForceProxyView;
}

/** Что произойдёт с секцией при взятии (диалог взятия). */
export interface SectionPreview {
  id: string;
  /** Файлы, которые уйдут в резервные копии. */
  move: string[];
  /** Файлы, которые остаются на месте. */
  left: string[];
  /** Файлы с комментариями, которые будут потеряны. */
  comments: string[];
  /** Секция заполнится значениями пресета: файла нет. */
  preset: boolean;
  blocked?: string;
  blocked_file?: string;
}

export interface TakePreview {
  kernel: CoreKernel;
  sections: SectionPreview[];
  force_proxy_tags?: string[];
}

/** Синтаксическая ошибка сырого блока. */
export interface RawError {
  line: number;
  detail: string;
}

/** Разбор сырого блока: значение и поля формы либо ошибка. В черновик ничего не пишется. */
export interface RawResult {
  rejected: KeyIssue[];
  value?: unknown;
  form?: unknown;
  error?: RawError;
}

/** Часть снимка слоя (поле cores): состояние секции Xray. */
export interface CoreSectionSnap {
  desired: boolean;
  taken: boolean;
  file: string;
  moved: string[];
  left: string[];
  drift: string[];
}

/** Объявление о способе перезапуска: ядро и причина (port, tun). */
export interface RestartNote {
  kernel: string;
  reason: 'port' | 'tun';
}

/** Поле cores снимка слоя. */
export interface CoresSnapshot {
  xray: Record<string, CoreSectionSnap>;
  restart: RestartNote[];
}

// --- Запросы ---

const BASE = '/api/configlayer/cores';

/** Вид секций ядра. */
export async function fetchCores(kernel: CoreKernel): Promise<KernelView> {
  return apiFetchJSON<KernelView>(`${BASE}?kernel=${encodeURIComponent(kernel)}`);
}

// --- Перечитывание вида ---

// Вид секций устаревает при правке черновика (draft), смене владельца маршрутизации
// (routing), изменении файлов и ходе «Применить». У слоя есть подписки на draft и routing;
// изменения files и apply приходят в снимок слоя, за ним и следим. Одна общая отложенная
// перезагрузка на все причины. configLayer этот модуль не импортирует — цикла нет.
const CORES_DEBOUNCE_MS = 300;

/** Подпись состояния файлов и применения в снимке слоя: меняется, когда вид мог устареть. */
function snapshotSignature(): string {
  const s = get(layerSnapshot);
  if (!s) return '';
  return `${s.files.map((f) => `${f.key}:${f.state}`).join('|')}#${s.apply.running ? 1 : 0}`;
}

/**
 * Вызывает cb (с задержкой 300 мс, одним вызовом на серию событий), когда вид секций мог
 * устареть. Возвращает функцию отписки.
 */
export function onCoresChanged(cb: () => void): () => void {
  let timer: ReturnType<typeof setTimeout> | null = null;
  const soon = (): void => {
    if (timer !== null) clearTimeout(timer);
    timer = setTimeout(() => {
      timer = null;
      cb();
    }, CORES_DEBOUNCE_MS);
  };
  const offDraft = onLayerEvent('draft', soon);
  const offRouting = onLayerEvent('routing', soon);
  let last = snapshotSignature();
  const offSnapshot = layerSnapshot.subscribe(() => {
    const next = snapshotSignature();
    if (next === last) return;
    last = next;
    soon();
  });
  return () => {
    offDraft();
    offRouting();
    offSnapshot();
    if (timer !== null) clearTimeout(timer);
    timer = null;
  };
}
