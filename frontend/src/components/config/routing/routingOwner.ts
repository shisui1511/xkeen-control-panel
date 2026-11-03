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
import {
  releaseRouting,
  routingErrorText,
  takeRouting,
  type TakeScope
} from '../../../lib/configRouting';
import {
  activateProfile,
  mihomoOwnerAction,
  profileErrorText,
  switchOutcomeToast,
  type ActivationResult
} from '../../../lib/configProfiles';
import { activateRestartGrace, clearRestartGrace } from '../../../lib/serviceGrace';

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
  /** Имя активного своего профиля Mihomo; пусто, пока им управляет панель или XKeen. */
  mihomoOwnProfile: string;
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
  // Свой профиль: активен файл не панели и не обычный config.yaml XKeen, взятия Mihomo нет
  const active = r?.mihomo_active ?? '';
  const own = active !== '' && active !== 'config.yaml' && r?.desired_mihomo !== true;
  return {
    kind,
    xrayInstalled: xray !== null,
    mihomoInstalled: mihomo !== null,
    xrayDrift: xray?.drift ?? [],
    mihomoDrift: mihomo?.drift ?? [],
    mihomoOwnProfile: own ? active : ''
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
 * Записывает в черновик желание «взять маршрутизацию» для области scope (по умолчанию оба
 * ядра). Файлы меняются при «Применить». Возвращает true, если желание записано; ошибка —
 * тост, состояние прежнее.
 */
export async function takeIntoDraft(scope: TakeScope = 'all'): Promise<boolean> {
  routingBusy.set(true);
  try {
    await takeRouting(scope);
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

/**
 * «Отпустить маршрутизацию»: подтверждение с последствиями, затем возврат и применение.
 * Пока Mihomo на своём профиле, тело говорит, что профиль не изменится (D-07).
 */
export async function confirmRelease(): Promise<void> {
  const tr = get(t);
  const own = get(ownerState).mihomoOwnProfile !== '';
  const ok = await showConfirm({
    variant: 'warning',
    title: tr('cfg.routing.release_title'),
    message: tr(own ? 'cfg.routing.release_body_own_profile' : 'cfg.routing.release_body'),
    confirmLabel: tr('cfg.routing.release_confirm')
  });
  if (ok) await releaseAndApply();
}

// --- Действия только с Mihomo (плашка профиля, D-06, D-07, D-09) ---

// Пока идёт действие Mihomo и ещё несколько секунд после него снимок слоя не даёт тостов
// «Маршрутизацию взяла панель» и «вернули XKeen»: смена владельца Mihomo — не то же самое.
// Infinity — действие идёт.
let ownerToastsMutedUntil = 0;
const OWNER_TOAST_TAIL_MS = 5000;

/** Причина отказа без завершающей точки: подставляется в предложение тоста. */
function plainReason(text: string): string {
  return text.replace(/\.\s*$/, '');
}

/**
 * Выполняет переход Mihomo под признаком «идёт запрос»: остальные действия вкладки неактивны,
 * окно перезапуска не поднимает баннер связи. Возвращает итог либо null, если запрос отклонён
 * (draft_open и др.): тогда причина уже в тосте onFail.
 */
async function runMihomoAction(
  call: () => Promise<ActivationResult>,
  onFail: (reason: string) => void
): Promise<ActivationResult | null> {
  routingBusy.set(true);
  ownerToastsMutedUntil = Infinity;
  activateRestartGrace(30000);
  try {
    const res = await call();
    // Без перезапуска (отказ, остановленное ядро) окно связи не держится
    if (!res.restarted) clearRestartGrace();
    return res;
  } catch (err) {
    clearRestartGrace();
    onFail(plainReason(profileErrorText(get(t), err)));
    return null;
  } finally {
    routingBusy.set(false);
    ownerToastsMutedUntil = Date.now() + OWNER_TOAST_TAIL_MS;
    void refetchLayerState();
  }
}

/** Текст подтверждения: тело плюс абзацы про Xray у панели и остановленный Mihomo. */
function confirmBody(body: string, xrayStays: boolean, stopped: boolean): string {
  const tr = get(t);
  const parts = [body];
  if (xrayStays) parts.push(tr('cfg.prof.xray_stays'));
  if (stopped) parts.push(tr('cfg.prof.confirm_stopped'));
  return parts.join(' ');
}

/**
 * «Отпустить Mihomo» (профиль у панели): подтверждение, затем возврат на прежний профиль и
 * применение сразу. Xray и его файлы не меняются. Итог — тост.
 */
export async function confirmMihomoRelease(name: string, stopped: boolean): Promise<void> {
  const tr = get(t);
  const ok = await showConfirm({
    variant: 'warning',
    title: tr('cfg.sec.mihomo_release_title'),
    message: confirmBody(
      tr('cfg.sec.mihomo_release_body', { name }),
      get(layerSnapshot)?.routing.xray?.taken === true,
      stopped
    ),
    confirmLabel: tr('cfg.sec.profile_release_btn'),
    cancelLabel: tr('app.cancel')
  });
  if (!ok) return;
  const failed = (reason: string) =>
    showToast('error', get(t)('cfg.sec.mihomo_release_failed', { reason }), 10000);
  const res = await runMihomoAction(() => mihomoOwnerAction('release'), failed);
  if (res === null) return;
  if (res.error) {
    failed(plainReason(res.error));
    return;
  }
  showToast('success', get(t)('cfg.sec.toast_mihomo_released', { name: res.active || name }), 6000);
  // Профиль записан, а ядро не перезапускалось (остановлено, неактивно): исход отдельным тостом
  if (res.outcome) switchOutcomeToast(res, res.active || name);
}

/**
 * «Управлять в панели» при сохранённых значениях секций (D-09): подтверждение, затем возврат
 * Mihomo панели и применение сразу. name — профиль, который останется в списке.
 */
export async function confirmMihomoReturn(name: string, stopped: boolean): Promise<void> {
  const tr = get(t);
  const ok = await showConfirm({
    variant: 'warning',
    title: tr('cfg.prof.to_panel_title'),
    message: confirmBody(tr('cfg.prof.to_panel_body', { name }), false, stopped),
    confirmLabel: tr('cfg.prof.to_panel_confirm'),
    cancelLabel: tr('app.cancel')
  });
  if (!ok) return;
  const res = await runMihomoAction(
    () => mihomoOwnerAction('return'),
    (reason) => showToast('error', `${get(t)('profiles.error')}: ${reason}`, 10000)
  );
  if (res !== null) switchOutcomeToast(res, res.active);
}

/**
 * «Сделать активным» из карточки «Профили»: подтверждение берёт сама карточка, здесь — запрос
 * под признаком «идёт запрос», окно перезапуска и тост по исходу (успех, откат, отказ проверки,
 * запись без перезапуска). Сервер сам выбирает путь по владельцу Mihomo; тосты смены владельца
 * на это время молчат. false — запрос отклонён (черновик открыт и др.), причина уже в тосте.
 */
export async function activateOwnProfile(name: string): Promise<boolean> {
  const res = await runMihomoAction(
    () => activateProfile(name),
    (reason) => showToast('error', `${get(t)('profiles.error')}: ${reason}`, 10000)
  );
  if (res === null) return false;
  switchOutcomeToast(res, name);
  return true;
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
    // Переход только Mihomo сообщает о себе своим тостом
    if (Date.now() < ownerToastsMutedUntil) return;
    const tr = get(t);
    showToast('success', tr(taken ? 'cfg.routing.toast_taken' : 'cfg.routing.toast_released'));
  });
}
