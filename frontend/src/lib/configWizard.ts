// Клиент мастера первого запуска (фаза 149.1, CFG-04): типы ответа GET
// /api/configlayer/wizard, стор статуса шагов и действия «Закрыть» и «Скрыть». Шаги 1–6,
// признак автооткрытия и видимость карточки «Настроить VPN» считает сервер (служба
// setupwizard): мастер и карточка дашборда только отражают пришедшее. Седьмой шаг
// (проверка IP) клиентский и сюда не входит (D-05). Событие `wizard` слоя несёт лишь
// счётчик, по нему вид перечитывается.

import { writable, get } from 'svelte/store';
import { apiFetchJSON } from './api';
import { onLayerEvent, post, layerSnapshot } from './configLayer';
import { activeKernelState, runningKernels } from '../stores';

// --- Типы контракта (поля как в JSON сервера) ---

/** Шаги мастера в прогрессе 1–6 (сервер). */
export type WizardStepId = 'kernel' | 'sections' | 'nodes' | 'routing' | 'apply' | 'start';

/** Признак мастера: пусто — не закрыт и не пройден. */
export type WizardStatus = '' | 'closed' | 'done' | 'hidden';

/** Шаги в порядке мастера. */
export const WIZARD_STEP_IDS: readonly WizardStepId[] = [
  'kernel',
  'sections',
  'nodes',
  'routing',
  'apply',
  'start'
];

/** Взято ли что-то у ядра в шаге «секции». */
export interface WizardKernelTake {
  done: boolean;
}

/** «Частично, N из M»: N секций Xray в черновике из M. */
export interface WizardPartial {
  done: number;
  total: number;
}

/** Один шаг мастера и его счётчики (необязательные поля — только у своего шага). */
export interface WizardStep {
  id: WizardStepId;
  done: boolean;
  kernels?: Partial<Record<'xray' | 'mihomo', WizardKernelTake>>;
  partial?: WizardPartial;
  nodes?: number;
  sources?: number;
  taken?: boolean;
  preset?: string;
  country?: string;
  draft_changes?: number;
  applied_empty?: boolean;
  apply_running?: boolean;
}

/** Установленность XKeen и ядер, запущенные ядра и конфликт (активного ядра здесь нет). */
export interface WizardKernels {
  xkeen: boolean;
  xray: boolean;
  mihomo: boolean;
  running: string[];
  conflict: boolean;
}

/** Ответ GET /api/configlayer/wizard. */
export interface WizardView {
  status: WizardStatus;
  empty: boolean;
  auto_open: boolean;
  card_visible: boolean;
  done_count: number;
  total: number;
  /** Номер (1…6) первого невыполненного шага; 0 — выполнены все. */
  current: number;
  kernels: WizardKernels;
  steps: WizardStep[];
}

// --- Сторы ---

/**
 * Последний вид мастера; null — ответа ещё нет. Карточка дашборда и
 * автооткрытие мастера ждут первого ответа и по null ничего не решают.
 */
export const wizardView = writable<WizardView | null>(null);
/** Чтение вида не удалось (прежний вид при сбое перечитывания остаётся на экране). */
export const wizardLoadError = writable(false);

// --- Запросы ---

const BASE = '/api/configlayer/wizard';

function asArray<T>(v: unknown): T[] {
  return Array.isArray(v) ? (v as T[]) : [];
}

/** Приводит ответ к полной форме: пропущенные списки становятся пустыми. */
function normalizeView(raw: WizardView): WizardView {
  const k = raw.kernels;
  return {
    ...raw,
    kernels: {
      xkeen: k?.xkeen === true,
      xray: k?.xray === true,
      mihomo: k?.mihomo === true,
      running: asArray<string>(k?.running),
      conflict: k?.conflict === true
    },
    steps: asArray<WizardStep>(raw.steps)
  };
}

// Номер чтения: поздний ответ устаревшего запроса не затирает свежий вид
let loadSeq = 0;

function setView(raw: WizardView): WizardView {
  const view = normalizeView(raw);
  loadSeq++;
  wizardView.set(view);
  wizardLoadError.set(false);
  return view;
}

/** Читает статус мастера. */
export async function loadWizard(): Promise<void> {
  const mine = ++loadSeq;
  try {
    const raw = await apiFetchJSON<WizardView>(BASE);
    if (mine !== loadSeq) return;
    setView(raw);
  } catch {
    if (mine !== loadSeq) return;
    // Прежний вид остаётся на экране; ошибка видна только без вида
    if (get(wizardView) === null) wizardLoadError.set(true);
  }
}

/** «Закрыть» мастер: отменяет автооткрытие (D-10), черновик не трогается. */
export async function closeWizard(): Promise<WizardView> {
  return setView(await post<WizardView>(`${BASE}/close`, {}));
}

/** «Скрыть» карточку «Настроить VPN»: действует как «пройдено» во всех браузерах (D-11). */
export async function hideWizard(): Promise<WizardView> {
  return setView(await post<WizardView>(`${BASE}/hide`, {}));
}

// --- Перечитывание вида ---

// Вид устаревает по событию wizard службы (смена признака или вектора шагов), по правке
// черновика, источников и маршрутизации, по ходу «Применить», по смене активного ядра и
// набора запущенных ядер.
// Одна общая отложенная перезагрузка на все причины; configLayer этот модуль не
// импортирует — цикла нет.
export const WIZARD_DEBOUNCE_MS = 300;

let users = 0;
let timer: ReturnType<typeof setTimeout> | null = null;
let unsubs: Array<() => void> = [];

function loadSoon(): void {
  if (timer !== null) clearTimeout(timer);
  timer = setTimeout(() => {
    timer = null;
    void loadWizard();
  }, WIZARD_DEBOUNCE_MS);
}

/** Подпись снимка слоя: меняется с ходом применения и числом изменений черновика. */
function snapshotSignature(): string {
  const s = get(layerSnapshot);
  if (!s) return '';
  return `${s.apply.running ? 1 : 0}#${s.draft_changes}`;
}

function subscribeAll(): void {
  unsubs.push(onLayerEvent('wizard', loadSoon));
  unsubs.push(onLayerEvent('draft', loadSoon));
  unsubs.push(onLayerEvent('sources', loadSoon));
  unsubs.push(onLayerEvent('routing', loadSoon));
  let lastSnap = snapshotSignature();
  unsubs.push(
    layerSnapshot.subscribe(() => {
      const next = snapshotSignature();
      if (next === lastSnap) return;
      lastSnap = next;
      loadSoon();
    })
  );
  let lastKernel = get(activeKernelState);
  unsubs.push(
    activeKernelState.subscribe((k) => {
      if (k === lastKernel) return;
      lastKernel = k;
      loadSoon();
    })
  );
  // Остановка или падение активного ядра не меняет activeKernelState, а шаг 6 от неё
  // зависит: перечитывание и по смене набора запущенных ядер.
  let lastRunning = get(runningKernels).join(',');
  unsubs.push(
    runningKernels.subscribe((r) => {
      const next = r.join(',');
      if (next === lastRunning) return;
      lastRunning = next;
      loadSoon();
    })
  );
}

function unsubscribeAll(): void {
  for (const off of unsubs) off();
  unsubs = [];
  if (timer !== null) clearTimeout(timer);
  timer = null;
}

function resetState(): void {
  loadSeq++;
  wizardView.set(null);
  wizardLoadError.set(false);
}

/**
 * Начинает слежение за статусом мастера (раздел, страница мастера, карточка дашборда).
 * Вызовы считаются: слежение идёт, пока есть хотя бы один пользователь; первый сразу
 * читает статус.
 */
export function startWizard(): void {
  users++;
  if (users > 1) return;
  subscribeAll();
  void loadWizard();
}

/** Снимает слежение одного пользователя; с последним — подписки и вид сбрасываются. */
export function stopWizard(): void {
  if (users === 0) return;
  users--;
  if (users > 0) return;
  unsubscribeAll();
  resetState();
}
