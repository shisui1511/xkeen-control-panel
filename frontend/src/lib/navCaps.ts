/**
 * navCaps.ts — минимальный срез /api/capabilities для кэша бокового меню
 * и проверка «установлено ли хоть одно ядро». Кэш влияет только на видимость
 * пунктов меню, не на авторизацию. Чтение и запись браузерного хранилища —
 * забота стора: модуль чистый и хранилища не касается.
 */

export const NAV_CAPS_KEY = 'xcp_nav_caps';
export const NAV_CAPS_VERSION = 1;

export type NavActiveKernel = 'xray' | 'mihomo' | 'none';

export interface NavCaps {
  v: 1;
  active_kernel: NavActiveKernel;
  kernels: { xray: boolean; mihomo: boolean };
  xkeen_installed?: boolean;
}

/** Структурный вход: подмножество CapabilitiesData без секретов и идентификаторов. */
export interface NavCapsSource {
  active_kernel?: string;
  kernels?: Record<string, { installed?: boolean } | undefined>;
  xkeen_installed?: boolean;
}

const ACTIVE_KERNELS: readonly string[] = ['xray', 'mihomo', 'none'];

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

/** Разбирает значение из хранилища; мусор, чужая версия, битый JSON → null. */
export function parseNavCaps(raw: string | null | undefined): NavCaps | null {
  if (!raw) return null;
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!isRecord(data) || data.v !== NAV_CAPS_VERSION) return null;
  if (typeof data.active_kernel !== 'string' || !ACTIVE_KERNELS.includes(data.active_kernel)) {
    return null;
  }
  const k = data.kernels;
  if (!isRecord(k) || typeof k.xray !== 'boolean' || typeof k.mihomo !== 'boolean') return null;
  const out: NavCaps = {
    v: 1,
    active_kernel: data.active_kernel as NavActiveKernel,
    kernels: { xray: k.xray, mihomo: k.mihomo }
  };
  if (typeof data.xkeen_installed === 'boolean') out.xkeen_installed = data.xkeen_installed;
  return out;
}

/** Копирует в срез только нужные поля; без kernels или с неизвестным ядром → null. */
export function toNavCaps(caps: NavCapsSource | null | undefined): NavCaps | null {
  if (!caps || !caps.kernels) return null;
  if (typeof caps.active_kernel !== 'string' || !ACTIVE_KERNELS.includes(caps.active_kernel)) {
    return null;
  }
  const out: NavCaps = {
    v: 1,
    active_kernel: caps.active_kernel as NavActiveKernel,
    kernels: {
      xray: caps.kernels.xray?.installed === true,
      mihomo: caps.kernels.mihomo?.installed === true
    }
  };
  if (typeof caps.xkeen_installed === 'boolean') out.xkeen_installed = caps.xkeen_installed;
  return out;
}

/** «none» в новом ответе (переходный момент) не затирает известное активное ядро. */
export function mergeNavCaps(prev: NavCaps | null, next: NavCaps): NavCaps {
  if (next.active_kernel === 'none' && prev && prev.active_kernel !== 'none') {
    return { ...next, active_kernel: prev.active_kernel };
  }
  return next;
}

/** Показывать ли пункты Mihomo в меню; null — срез неизвестен. */
export function showMihomoNavFor(nav: NavCaps | null): boolean | null {
  if (!nav) return null;
  return nav.active_kernel !== 'xray';
}

/**
 * Установлено ли хоть одно ядро. null — capabilities неизвестны (или kernels
 * пуст): пока так, действия не блокируются.
 */
export function anyKernelInstalled(
  caps: { kernels?: Record<string, { installed?: boolean } | undefined> } | null | undefined
): boolean | null {
  const kernels = caps?.kernels;
  if (!kernels) return null;
  const list = Object.values(kernels);
  if (list.length === 0) return null;
  return list.some((k) => k?.installed === true);
}
