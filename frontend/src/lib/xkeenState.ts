/**
 * xkeenState.ts — единое состояние XKeen для дашборда и страницы «Сервисы»:
 * из capabilities и кэшированного статуса. Пока ответа нет или кэш холодный,
 * состояние «unknown» — шаги «Настройте»/«Запустите» в этом случае не выдаются.
 */

import { isColdUnknown, type ServiceStatusData } from './serviceStatus';

export type XKeenState = 'not_installed' | 'setup_incomplete' | 'running' | 'stopped' | 'unknown';

export type XKeenCardStatus = 'not_installed' | 'running' | 'stopped' | 'unknown';

export interface XKeenCaps {
  xkeen_installed?: boolean;
}

export function xkeenState(
  caps: XKeenCaps | null | undefined,
  svc: ServiceStatusData | null | undefined
): XKeenState {
  if (caps?.xkeen_installed === false) return 'not_installed';
  if (!svc) return 'unknown';
  if (svc.xkeen_installed === false) return 'not_installed';
  if (svc.xkeen_setup_incomplete) return 'setup_incomplete';
  if (svc.is_running) return 'running';
  if (isColdUnknown(svc)) return 'unknown';
  return 'stopped';
}

/** Прерванная установка на карточке выглядит как остановленная служба. */
export function xkeenCardStatus(state: XKeenState): XKeenCardStatus {
  return state === 'setup_incomplete' ? 'stopped' : state;
}

export interface XKeenVersionLabel {
  key?: string;
  text?: string;
}

export function xkeenVersionLabel(
  state: XKeenState,
  version: string | undefined
): XKeenVersionLabel {
  if (state === 'not_installed') return { key: 'dash.xkeen_version_not_installed' };
  if (!version || version === 'unknown') return { text: '—' };
  return { text: version };
}
