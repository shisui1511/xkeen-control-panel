/**
 * nextStep.ts — «лестница шагов» дашборда чистой системы: что делать
 * пользователю дальше. Неизвестные входы (null / unknown) шагов не порождают.
 */

import type { XKeenState } from './xkeenState';

export type NextStep =
  'install_xkeen' | 'finish_xkeen_setup' | 'install_kernel' | 'configure' | 'start';

export interface NextStepInput {
  xkeen: XKeenState;
  /** true — хотя бы одно ядро установлено, false — ни одного, null — неизвестно. */
  kernelsInstalled: boolean | null;
  /** true — в конфиге активного ядра есть подключения, false — нет, null — неизвестно. */
  configReady: boolean | null;
  isRunning: boolean;
}

export function nextStep(input: NextStepInput): NextStep | null {
  switch (input.xkeen) {
    case 'not_installed':
      return 'install_xkeen';
    case 'setup_incomplete':
      return 'finish_xkeen_setup';
    case 'unknown':
      return null;
  }
  if (input.kernelsInstalled === false) return 'install_kernel';
  if (input.xkeen === 'running' || input.isRunning) return null;
  if (input.configReady === false) return 'configure';
  // «Запустите» только когда известно, что в конфигурации есть подключения
  return input.configReady === true ? 'start' : null;
}

const NO_CONNECTIONS_CODE: Record<string, string> = {
  xray: 'no_real_outbounds',
  mihomo: 'no_proxies_or_providers'
};

interface Issue {
  code?: unknown;
}

function hasCode(list: unknown, code: string): boolean {
  return Array.isArray(list) && list.some((i: Issue) => i?.code === code);
}

/**
 * Разбор ответа GET /api/config/preflight: есть ли в конфиге активного ядра
 * подключения. Непустые errors или предупреждение «нет подключений» → false;
 * ответ не той формы → null.
 */
export function preflightConfigReady(
  kernel: 'xray' | 'mihomo',
  preflight: unknown
): boolean | null {
  if (typeof preflight !== 'object' || preflight === null) return null;
  const p = preflight as { valid?: unknown; errors?: unknown; warnings?: unknown };
  if (!Array.isArray(p.errors) && !Array.isArray(p.warnings) && typeof p.valid !== 'boolean')
    return null;
  if (Array.isArray(p.errors) && p.errors.length > 0) return false;
  if (p.valid === false) return false;
  return !hasCode(p.warnings, NO_CONNECTIONS_CODE[kernel]);
}
