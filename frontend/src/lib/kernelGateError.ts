/**
 * kernelGateError.ts — единственное место, где ответы 409 гейта ядра превращаются
 * в текст на языке интерфейса (D-06). Различаем только по машинному `code`,
 * не по статусу 409: другие 409 сервера (конфликт имён, занятый файл) идут прежним путём.
 * Значения `required` и `active` приходят с сервера и в текст попадают только из
 * белого списка имён ядер через kernelLabel (T-140.1-05).
 */
import { kernelLabel } from './kernelState';

export const KERNEL_GATE_CODES = [
  'kernel_inactive',
  'kernel_conflict',
  'kernel_op_in_progress'
] as const;
export type KernelGateCode = (typeof KERNEL_GATE_CODES)[number];

export interface KernelGateMeta {
  code?: string;
  required?: string;
  active?: string;
}

export type Translate = (key: string, params?: Record<string, string>) => string;

const REQUIRED_KERNELS: readonly string[] = ['xray', 'mihomo'];
const ACTIVE_KERNELS: readonly string[] = ['xray', 'mihomo', 'none', 'both'];

function isGateCode(code: unknown): code is KernelGateCode {
  return typeof code === 'string' && (KERNEL_GATE_CODES as readonly string[]).includes(code);
}

/** Текст для ответа гейта ядра; null — это не ответ гейта, вызывающий идёт в обычную ветку. */
export function kernelGateMessage(
  meta: KernelGateMeta | null | undefined,
  tr: Translate
): string | null {
  if (!meta || !isGateCode(meta.code)) return null;

  switch (meta.code) {
    case 'kernel_conflict':
      return tr('kernel.conflict_blocked_toast');
    case 'kernel_op_in_progress':
      return tr('kernel.op_in_progress');
    case 'kernel_inactive': {
      if (typeof meta.required !== 'string' || !REQUIRED_KERNELS.includes(meta.required)) {
        return null;
      }
      const required = kernelLabel(meta.required);
      const active =
        typeof meta.active === 'string' && ACTIVE_KERNELS.includes(meta.active)
          ? meta.active
          : 'none';
      if (active === 'both') return tr('kernel.conflict_blocked_toast');
      if (active === 'none') return tr('kernel.inactive_none', { required });
      return tr('kernel.inactive_wrong', { active: kernelLabel(active), required });
    }
  }
  return null;
}

/**
 * Разбирает тело ответа 409 для прямых fetch-клиентов (без конверта apiFetchJSON).
 * Только при статусе 409 и коде из белого списка; любая ошибка разбора — null.
 */
export async function readKernelGateMeta(res: Response): Promise<KernelGateMeta | null> {
  if (res.status !== 409) return null;
  try {
    const body = await res.clone().json();
    if (!body || typeof body !== 'object' || !isGateCode(body.code)) return null;
    const meta: KernelGateMeta = { code: body.code };
    if (typeof body.required === 'string') meta.required = body.required;
    if (typeof body.active === 'string') meta.active = body.active;
    return meta;
  } catch {
    return null;
  }
}
