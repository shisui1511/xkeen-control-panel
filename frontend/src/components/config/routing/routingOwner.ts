// Владение маршрутизацией в интерфейсе (D-01…D-07): кто управляет, действия плашки и
// диалога взятия, тосты по итогу применения. Решения принимает сервер — здесь только
// отражение снимка слоя и вызовы маршрутов configRouting.ts.

import { derived, get, writable, type Readable } from 'svelte/store';
import { t } from '../../../i18n';
import { showConfirm, showToast } from '../../../stores';
import {
  applyNow,
  handleLayerError,
  layerSnapshot,
  refetchLayerState
} from '../../../lib/configLayer';
import { releaseRouting, routingErrorText, takeRouting } from '../../../lib/configRouting';

/** Кто управляет маршрутизацией: ни одного установленного ядра, XKeen, панель или взятие ждёт «Применить». */
export type OwnerKind = 'none' | 'xkeen' | 'pending' | 'panel';

export interface OwnerState {
  kind: OwnerKind;
  xrayInstalled: boolean;
  mihomoInstalled: boolean;
  /** Файлы routing Xray, мешающие файлу панели (дрейф). */
  xrayDrift: string[];
  /** Расхождение профиля Mihomo. */
  mihomoDrift: string[];
}

/** Состояние владения по снимку слоя; пока снимка нет, ядер «нет». */
export const ownerState: Readable<OwnerState> = derived(layerSnapshot, (s) => {
  const r = s?.routing;
  const xray = r?.xray ?? null;
  const mihomo = r?.mihomo ?? null;
  const installed = xray !== null || mihomo !== null;
  const taken = xray?.taken === true || mihomo?.taken === true;
  let kind: OwnerKind = 'none';
  if (installed) kind = taken ? 'panel' : r?.desired ? 'pending' : 'xkeen';
  return {
    kind,
    xrayInstalled: xray !== null,
    mihomoInstalled: mihomo !== null,
    xrayDrift: xray?.drift ?? [],
    mihomoDrift: mihomo?.drift ?? []
  };
});

/** Идёт запрос взятия, возврата или решения дрейфа: остальные действия вкладки неактивны. */
export const routingBusy = writable(false);

/** Причина ошибки действия без завершающей точки: текст подставляется в предложение. */
export function reasonOf(err: unknown): string {
  const e = err as { code?: string; params?: Record<string, string>; message?: string } | null;
  const text =
    e && typeof e.code === 'string' && e.code !== ''
      ? routingErrorText(get(t), e.code, e.params)
      : e && typeof e.message === 'string' && e.message !== ''
        ? e.message
        : routingErrorText(get(t), undefined);
  return text.replace(/\.\s*$/, '');
}

function failToast(key: 'cfg.routing.take_failed' | 'cfg.routing.release_failed', err: unknown) {
  showToast('error', get(t)(key, { reason: reasonOf(err) }), 6000);
  // Устаревшая ревизия: состояние перечитывается, правка не потеряна молча
  if ((err as { code?: unknown } | null)?.code === 'draft_conflict') void refetchLayerState();
}

/**
 * Записывает в черновик желание «взять маршрутизацию». Файлы меняются при «Применить».
 * Возвращает true, если желание записано; ошибка — тост, состояние прежнее.
 */
export async function takeIntoDraft(): Promise<boolean> {
  routingBusy.set(true);
  try {
    await takeRouting();
    return true;
  } catch (err) {
    failToast('cfg.routing.take_failed', err);
    return false;
  } finally {
    routingBusy.set(false);
  }
}

/** Записывает желание «вернуть XKeen» и сразу применяет: возврат файлов не ждёт (D-01). */
export async function releaseAndApply(): Promise<void> {
  routingBusy.set(true);
  try {
    await releaseRouting();
  } catch (err) {
    failToast('cfg.routing.release_failed', err);
    return;
  } finally {
    routingBusy.set(false);
  }
  try {
    await applyNow();
  } catch (err) {
    handleLayerError(err);
  }
}

/** «Отпустить маршрутизацию»: подтверждение с последствиями, затем возврат и применение. */
export async function confirmRelease(): Promise<void> {
  const tr = get(t);
  const ok = await showConfirm({
    variant: 'warning',
    title: tr('cfg.routing.release_title'),
    message: tr('cfg.routing.release_body'),
    confirmLabel: tr('cfg.routing.release_confirm')
  });
  if (ok) await releaseAndApply();
}

/**
 * Выполняет действие решения дрейфа (reclaim) под признаком «идёт запрос»: остальные
 * действия вкладки неактивны. Ошибка — тост с текстом по коду; снимок слоя перечитывается.
 */
export async function guarded(action: () => Promise<void>): Promise<void> {
  routingBusy.set(true);
  try {
    await action();
    await refetchLayerState();
  } catch (err) {
    showToast('error', routingErrorText(get(t), (err as { code?: string })?.code, paramsOf(err)));
  } finally {
    routingBusy.set(false);
  }
}

function paramsOf(err: unknown): Record<string, string> | undefined {
  const p = (err as { params?: unknown } | null)?.params;
  return p && typeof p === 'object' ? (p as Record<string, string>) : undefined;
}

/** Запускает «Применить» из диалога или плашки; отказ — обычный разбор ошибок слоя. */
export async function applyFromUi(): Promise<void> {
  try {
    await applyNow();
  } catch (err) {
    handleLayerError(err);
  }
}

// --- Тосты по итогу применения ---

/**
 * Тосты «Маршрутизацию взяла панель.» и «Маршрутизацию вернули XKeen.» при смене
 * фактического владения в снимке (после применения). Первый снимок и применение,
 * вызванное установкой ядра (об этом говорит уведомление), тоста не дают. Возвращает
 * функцию отписки.
 */
export function watchOwnerToasts(): () => void {
  let prev: boolean | null = null;
  return layerSnapshot.subscribe((s) => {
    if (s === null) {
      prev = null;
      return;
    }
    const taken = s.routing.xray?.taken === true || s.routing.mihomo?.taken === true;
    const installed = s.routing.xray !== null || s.routing.mihomo !== null;
    const was = prev;
    prev = installed ? taken : null;
    if (was === null || !installed || was === taken) return;
    if (s.apply.trigger === 'kernel_installed') return;
    const tr = get(t);
    showToast('success', tr(taken ? 'cfg.routing.toast_taken' : 'cfg.routing.toast_released'));
  });
}
