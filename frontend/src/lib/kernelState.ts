/**
 * kernelState.ts — чистая модель «какое ядро активно» (без Svelte и сторов).
 * Единственное место, где ответ capabilities превращается в состояние ядра:
 * xray / mihomo / none / conflict. Конфликт — два запущенных ядра сразу; сервер
 * сообщает его флагом kernel_conflict и значением active_kernel "both".
 */

export type KernelState = 'xray' | 'mihomo' | 'none' | 'conflict';

/** Известные имена ядер в порядке показа. */
export const KERNEL_NAMES = ['xray', 'mihomo'] as const;
export type KernelName = (typeof KERNEL_NAMES)[number];

/** Структурный вход: подмножество CapabilitiesData. */
export interface KernelStateSource {
  active_kernel?: string;
  kernel_conflict?: boolean;
  running_kernels?: string[];
}

/** null — ответа capabilities ещё не было (состояние неизвестно). */
export function kernelStateOf(caps: KernelStateSource | null | undefined): KernelState | null {
  if (!caps) return null;
  if (caps.kernel_conflict === true || caps.active_kernel === 'both') return 'conflict';
  if (caps.active_kernel === 'xray' || caps.active_kernel === 'mihomo') return caps.active_kernel;
  return 'none';
}

/** Имя единственного активного ядра; при конфликте, none и до ответа — пустая строка. */
export function kernelNameOf(state: KernelState | null): KernelName | '' {
  return state === 'xray' || state === 'mihomo' ? state : '';
}

/** Подпись ядра в регистре бренда; неизвестное значение возвращается как есть. */
export function kernelLabel(kernel: string): string {
  if (kernel === 'xray') return 'Xray';
  if (kernel === 'mihomo') return 'Mihomo';
  return kernel;
}

/** Запущенные ядра в порядке xray, mihomo; при конфликте без поля — оба. */
export function runningKernelsOf(caps: KernelStateSource | null | undefined): KernelName[] {
  if (!caps) return [];
  if (Array.isArray(caps.running_kernels)) {
    return KERNEL_NAMES.filter((k) => caps.running_kernels!.includes(k));
  }
  if (kernelStateOf(caps) === 'conflict') return [...KERNEL_NAMES];
  return [];
}
