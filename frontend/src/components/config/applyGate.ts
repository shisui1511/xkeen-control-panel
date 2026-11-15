// Причины, по которым «Применить» недоступна (149 D-08, D-15): один производный стор для
// полосы черновика и шага 5 мастера, чтобы причины не расходились между копиями. Решение
// о применении принимает сервер — здесь только отражение состояния слоя в интерфейсе.

import { derived, type Readable } from 'svelte/store';
import { conflictVisible } from '../../stores';
import {
  applyRunning,
  applyStartedHere,
  draftChanges,
  driftCount,
  geodataHolder,
  layerConnection,
  layerSnapshot,
  layerStatus
} from '../../lib/configLayer';
import { routingBusy } from './routing/routingOwner';

/** Держатель общего замка менеджера geodata (D-11): -ug, уборка записи, запись xkeen.json. */
export type ApplyHolder = '' | 'geodata_update' | 'geodata_remove' | 'geodata_write';

/** Главная причина блокировки — в порядке приоритета подсказки кнопки. */
export type ApplyBlockReason = 'lost' | 'conflict' | 'holder' | 'checks' | 'drift' | '';

export interface ApplyGate {
  /** Состояние слоя загружено. */
  ready: boolean;
  /** Снимок слоя уже есть (в том числе во время перечитывания). */
  hasSnapshot: boolean;
  /** Первая загрузка: снимка ещё нет и ошибки нет. */
  firstLoad: boolean;
  /** Перечитывание поверх уже показанного снимка. */
  refreshing: boolean;
  /** В черновике есть неприменённые изменения. */
  dirty: boolean;
  /** Связь со слоем потеряна: состояние черновика на сервере неизвестно. */
  lost: boolean;
  /** Применение идёт из этой вкладки (флаг ставится до ответа сервера). */
  applyingHere: boolean;
  /** Применение идёт из другой вкладки (D-16). */
  applyingOther: boolean;
  /** Кто держит замок geodata; пусто — замок свободен. */
  holder: ApplyHolder;
  /** Число проблем проверки черновика (пустая группа, нет списка, цикл). */
  checkCount: number;
  /** Число файлов ядер, изменённых вне панели. */
  driftCount: number;
  /** Запущены оба ядра (D-18): окно изменений не открывается. */
  blockedByConflict: boolean;
  /** Файлы ядер изменены вне панели. */
  blockedByDrift: boolean;
  /** Проверка черновика нашла проблемы (GRP-05, SRC-07). */
  blockedByChecks: boolean;
  /** Применение идёт здесь или в другой вкладке либо идёт действие владения маршрутизацией. */
  busy: boolean;
  /** «Применить» можно нажать. */
  canApply: boolean;
  /** Главная причина блокировки для подсказки; пусто — блокировки нет. */
  reason: ApplyBlockReason;
}

/** Функция перевода в форме стора `t`. */
type Translate = (key: string, params?: Record<string, string | number>) => string;

function holderOf(raw: string): ApplyHolder {
  return raw === 'geodata_update' || raw === 'geodata_remove' || raw === 'geodata_write' ? raw : '';
}

export const applyGate: Readable<ApplyGate> = derived(
  [
    draftChanges,
    driftCount,
    applyRunning,
    applyStartedHere,
    layerSnapshot,
    layerStatus,
    layerConnection,
    geodataHolder,
    conflictVisible,
    routingBusy
  ],
  ([
    $draftChanges,
    $driftCount,
    $applyRunning,
    $applyStartedHere,
    $layerSnapshot,
    $layerStatus,
    $layerConnection,
    $geodataHolder,
    $conflictVisible,
    $routingBusy
  ]): ApplyGate => {
    const ready = $layerStatus === 'ready';
    const hasSnapshot = $layerSnapshot !== null;
    const firstLoad = !hasSnapshot && $layerStatus !== 'error';
    const refreshing = hasSnapshot && $layerStatus === 'loading';
    const dirty = $draftChanges > 0;
    const lost = $layerConnection === 'lost';
    const applyingHere = $applyStartedHere;
    const applyingOther = $applyRunning && !$applyStartedHere;
    const holder = holderOf($geodataHolder);
    const checkCount = $layerSnapshot?.checks.length ?? 0;
    const blockedByConflict = dirty && $conflictVisible;
    const blockedByDrift = dirty && $driftCount > 0;
    const blockedByChecks = dirty && checkCount > 0;
    const busy = applyingHere || applyingOther || $routingBusy;
    const reason: ApplyBlockReason = lost
      ? 'lost'
      : blockedByConflict
        ? 'conflict'
        : holder !== ''
          ? 'holder'
          : blockedByChecks
            ? 'checks'
            : blockedByDrift
              ? 'drift'
              : '';
    const canApply =
      ready &&
      dirty &&
      !busy &&
      !lost &&
      holder === '' &&
      !blockedByConflict &&
      !blockedByDrift &&
      !blockedByChecks;
    return {
      ready,
      hasSnapshot,
      firstLoad,
      refreshing,
      dirty,
      lost,
      applyingHere,
      applyingOther,
      holder,
      checkCount,
      driftCount: $driftCount,
      blockedByConflict,
      blockedByDrift,
      blockedByChecks,
      busy,
      canApply,
      reason
    };
  }
);

/** Подпись держателя замка geodata; пусто — замок свободен. */
export function applyHolderHint(g: ApplyGate, tr: Translate): string {
  switch (g.holder) {
    case 'geodata_update':
      return tr('cfg.geo.ug_busy');
    case 'geodata_remove':
      return tr('cfg.routing.apply_busy_hint');
    case 'geodata_write':
      return tr('cfg.geo.write_busy');
    default:
      return '';
  }
}

/** Подсказка кнопки «Применить»: главная причина блокировки или обычная подпись. */
export function applyGateHint(g: ApplyGate, tr: Translate): string {
  switch (g.reason) {
    case 'lost':
      return tr('cfg.sse_lost');
    case 'conflict':
      return tr('cfg.apply_blocked_conflict');
    case 'holder':
      return applyHolderHint(g, tr);
    case 'checks':
      return tr('cfg.routing.apply_blocked_hint');
    case 'drift':
      return tr('cfg.apply_blocked_drift', { k: g.driftCount });
    default:
      return tr('cfg.apply_aria');
  }
}
