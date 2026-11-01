// Владение секциями ядер в интерфейсе (D-01…D-05): признак «идёт запрос», защищённый
// вызов действия, подтверждения «Отпустить управление» и «Заполнить из пресета». Решения
// принимает сервер — здесь только вызовы маршрутов configCores.ts и разбор их ошибок.
// Все действия пишут в черновик: файлы меняются при «Применить».

import { get, writable } from 'svelte/store';
import { t } from '../../../i18n';
import { showConfirm, showToast } from '../../../stores';
import { applyRunning, handleLayerError, refetchLayerState } from '../../../lib/configLayer';
import {
  coresErrorText,
  presetSection,
  reclaimSection,
  releaseSection,
  takeSections,
  type CoreKernel
} from '../../../lib/configCores';

/** Идёт запрос действия владельца секции: остальные кнопки вкладки неактивны. */
export const coresBusy = writable(false);

function paramsOf(err: unknown): Record<string, string> | undefined {
  const p = (err as { params?: unknown } | null)?.params;
  return p && typeof p === 'object' ? (p as Record<string, string>) : undefined;
}

/**
 * Разбор ошибки действия с секцией: устаревшая ревизия и занятый замок — обычный разбор
 * слоя (тост и перечитывание), прочее — тост с текстом по коду.
 */
export function failCores(err: unknown): void {
  const code = (err as { code?: string } | null)?.code;
  if (code === 'draft_conflict' || code === 'apply_busy') {
    handleLayerError(err);
    return;
  }
  showToast('error', coresErrorText(get(t), code, paramsOf(err)), 6000);
}

/**
 * Выполняет действие под признаком «идёт запрос». Пока идёт запрос или «Применить»,
 * повторный вызов ничего не делает. После успеха состояние слоя перечитывается; возвращает
 * true, если действие выполнено.
 */
export async function guarded(action: () => Promise<unknown>): Promise<boolean> {
  if (get(coresBusy) || get(applyRunning)) return false;
  coresBusy.set(true);
  try {
    await action();
    await refetchLayerState();
    return true;
  } catch (err) {
    failCores(err);
    return false;
  } finally {
    coresBusy.set(false);
  }
}

/** «Отпустить управление»: подтверждение с последствиями, затем желание «вернуть» в черновик. */
export async function confirmReleaseSection(
  kernel: CoreKernel,
  id: string,
  name: string
): Promise<boolean> {
  const tr = get(t);
  const ok = await showConfirm({
    variant: 'warning',
    title: tr('cfg.sec.release_title', { name }),
    message: tr('cfg.sec.release_body'),
    confirmLabel: tr('cfg.sec.release_confirm')
  });
  if (!ok) return false;
  return guarded(() => releaseSection(kernel, id));
}

/** «Заполнить из пресета»: подтверждение перезаписи, затем значения пресета в черновик. */
export async function confirmPresetSection(
  kernel: CoreKernel,
  id: string,
  name: string
): Promise<boolean> {
  const tr = get(t);
  const ok = await showConfirm({
    variant: 'warning',
    title: tr('cfg.sec.preset_title', { name }),
    message: tr('cfg.sec.preset_body'),
    confirmLabel: tr('cfg.sec.preset_confirm')
  });
  if (!ok) return false;
  return guarded(() => presetSection(kernel, id));
}

/**
 * «Управлять» у секции, отпускание которой ещё не применено: желание «вернуть» снимается,
 * секция остаётся у панели с прежним значением (сервер возвращает его из применённого).
 */
export function cancelRelease(kernel: CoreKernel, id: string): Promise<boolean> {
  return guarded(() => takeSections(kernel, [id], false));
}

/** Решает дрейф секции в пользу панели: файл панели возвращается на диск сразу. */
export function reclaim(kernel: CoreKernel, id: string): Promise<boolean> {
  return guarded(() => reclaimSection(kernel, id));
}
