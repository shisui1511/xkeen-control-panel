/**
 * awgPresets.ts — Пресеты обфускации AmneziaWG, генератор валидных параметров и анализатор различий (diff).
 */

import { isMihomoAwg31Supported } from './awgFields';

export type AwgPresetId =
  'standard' | 'mobile-lte' | 'aggressive-dpi' | 'compatibility' | 'low-ram-mips';

export interface AwgPreset {
  id: AwgPresetId;
  nameKey: string;
  descKey: string;
  tier: 'classic' | '2.0' | '3.1';
  mtu: number;
  options: {
    jc: number;
    jmin: number;
    jmax: number;
    s1: number;
    s2: number;
    s3?: number;
    s4?: number;
    h1: number | string;
    h2: number | string;
    h3: number | string;
    h4: number | string;
    // 3.1 опции
    version?: string;
    headerProtectionKey?: string;
    randomTrailers?: boolean;
    disableCookies?: boolean;
  };
}

export const AWG_PRESETS: Record<AwgPresetId, AwgPreset> = {
  standard: {
    id: 'standard',
    nameKey: 'proxies.awg_preset_standard',
    descKey: 'proxies.awg_preset_standard_desc',
    tier: '2.0',
    mtu: 1280,
    options: {
      jc: 4,
      jmin: 40,
      jmax: 70,
      s1: 15,
      s2: 40,
      s3: 8,
      s4: 12,
      h1: 1000000001,
      h2: 1000000002,
      h3: 1000000003,
      h4: 1000000004
    }
  },
  'mobile-lte': {
    id: 'mobile-lte',
    nameKey: 'proxies.awg_preset_mobile_lte',
    descKey: 'proxies.awg_preset_mobile_lte_desc',
    tier: 'classic',
    mtu: 1280,
    options: {
      jc: 2,
      jmin: 20,
      jmax: 40,
      s1: 20,
      s2: 50,
      h1: 1111111111,
      h2: 2222222222,
      h3: 3333333333,
      h4: 4444444444
    }
  },
  'aggressive-dpi': {
    id: 'aggressive-dpi',
    nameKey: 'proxies.awg_preset_aggressive_dpi',
    descKey: 'proxies.awg_preset_aggressive_dpi_desc',
    tier: '3.1',
    mtu: 1280,
    options: {
      jc: 8,
      jmin: 60,
      jmax: 120,
      s1: 30,
      s2: 120,
      s3: 15,
      s4: 25,
      h1: '1000000000-1500000000',
      h2: '1500000001-2000000000',
      h3: '2000000001-2500000000',
      h4: '2500000001-3000000000',
      version: '3.1',
      randomTrailers: true
    }
  },
  compatibility: {
    id: 'compatibility',
    nameKey: 'proxies.awg_preset_compatibility',
    descKey: 'proxies.awg_preset_compatibility_desc',
    tier: 'classic',
    mtu: 1280,
    options: {
      jc: 4,
      jmin: 40,
      jmax: 70,
      s1: 15,
      s2: 40,
      h1: 1000000001,
      h2: 1000000002,
      h3: 1000000003,
      h4: 1000000004
    }
  },
  'low-ram-mips': {
    id: 'low-ram-mips',
    nameKey: 'proxies.awg_preset_low_ram_mips',
    descKey: 'proxies.awg_preset_low_ram_mips_desc',
    tier: 'classic',
    mtu: 1280,
    options: {
      jc: 1,
      jmin: 10,
      jmax: 30,
      s1: 15,
      s2: 40,
      h1: 1000000001,
      h2: 1000000002,
      h3: 1000000003,
      h4: 1000000004
    }
  }
};

/**
 * Возвращает параметры пресета, адаптированные под версию ядра Mihomo.
 */
export function getAwgPreset(id: AwgPresetId, kernelVersion?: string | null): Record<string, any> {
  const preset = AWG_PRESETS[id];
  if (!preset) return {};

  const res = { ...preset.options, mtu: preset.mtu };
  const supports31 = isMihomoAwg31Supported(kernelVersion);

  if (!supports31) {
    // Удаляем 3.1 поля, если ядро не поддерживает AmneziaWG 3.1
    delete (res as any).version;
    delete (res as any).headerProtectionKey;
    delete (res as any).randomTrailers;
    delete (res as any).disableCookies;

    // Преобразуем строковые диапазоны h1..h4 в обычные числа
    for (const h of ['h1', 'h2', 'h3', 'h4'] as const) {
      if (typeof res[h] === 'string') {
        const parts = (res[h] as string).split('-');
        const val = parseInt(parts[0], 10);
        (res as any)[h] = isNaN(val) ? 1000000000 : val;
      }
    }
  } else if (id === 'aggressive-dpi' && !res.headerProtectionKey) {
    // Генерируем свежий заголовочный ключ для 3.1
    res.headerProtectionKey = generateRandomHex(32);
  }

  return res;
}

/**
 * Генерирует случайную hex-строку заданной длины в байтах.
 */
export function generateRandomHex(bytesCount: number): string {
  if (typeof crypto !== 'undefined' && crypto.getRandomValues) {
    const arr = new Uint8Array(bytesCount);
    crypto.getRandomValues(arr);
    return Array.from(arr)
      .map((b) => b.toString(16).padStart(2, '0'))
      .join('')
      .toUpperCase();
  }
  let str = '';
  for (let i = 0; i < bytesCount; i++) {
    str += Math.floor(Math.random() * 256)
      .toString(16)
      .padStart(2, '0');
  }
  return str.toUpperCase();
}

/**
 * Генерирует свежий случайный, математически валидный набор параметров AmneziaWG.
 * Гарантирует:
 * - S1 + 56 != S2
 * - Все H1..H4 уникальны (не равны друг другу)
 * - Jmin <= Jmax
 * - При наличии 3.1: S1..S4 >= 12
 */
export function generateRandomAwgParams(kernelVersion?: string | null): Record<string, any> {
  const supports31 = isMihomoAwg31Supported(kernelVersion);

  const jc = Math.floor(Math.random() * 6) + 3; // 3..8
  const jmin = Math.floor(Math.random() * 31) + 20; // 20..50
  const jmax = jmin + Math.floor(Math.random() * 51) + 20; // jmin + 20..70

  const s1 = Math.floor(Math.random() * 31) + (supports31 ? 15 : 10); // 10..40 (15..45 if 3.1)
  let s2 = Math.floor(Math.random() * 51) + 30; // 30..80

  // Гарантия: S1 + 56 != S2
  if (s1 + 56 === s2) {
    s2 += 7;
  }

  const s3 = Math.floor(Math.random() * 15) + (supports31 ? 12 : 5);
  const s4 = Math.floor(Math.random() * 20) + (supports31 ? 12 : 8);

  // Генерация 4 уникальных значений H1..H4
  const usedHeaders = new Set<number>();
  const headers: number[] = [];
  while (headers.length < 4) {
    // Число в диапазоне 1000000000..2100000000
    const h = 1000000000 + Math.floor(Math.random() * 1100000000);
    if (!usedHeaders.has(h)) {
      usedHeaders.add(h);
      headers.push(h);
    }
  }

  const result: Record<string, any> = {
    jc,
    jmin,
    jmax,
    s1,
    s2,
    s3,
    s4,
    h1: headers[0],
    h2: headers[1],
    h3: headers[2],
    h4: headers[3],
    mtu: 1280
  };

  if (supports31) {
    result.version = '3.1';
    result.headerProtectionKey = generateRandomHex(32);
    result.randomTrailers = true;
  }

  return result;
}

export interface AwgDiffResult {
  kept: { key: string; val: any }[];
  warnings: { key: string; val: any; messageKey: string }[];
  dropped: { key: string; val: any; reasonKey: string }[];
}

/**
 * Анализирует параметры AWG при импорте/конвертации для целевого ядра.
 */
export function analyzeAwgDiff(
  rawAwg: Record<string, any>,
  targetKernel: 'mihomo' | 'xray',
  kernelVersion?: string | null,
  mtu = 1280
): AwgDiffResult {
  const kept: { key: string; val: any }[] = [];
  const warnings: { key: string; val: any; messageKey: string }[] = [];
  const dropped: { key: string; val: any; reasonKey: string }[] = [];

  const supports31 = targetKernel === 'mihomo' && isMihomoAwg31Supported(kernelVersion);

  for (const [k, val] of Object.entries(rawAwg)) {
    if (val === undefined || val === null || val === '') continue;
    const lowerKey = k.toLowerCase().replace(/[-_]/g, '');

    if (targetKernel === 'xray') {
      // Xray отбрасывает всю специфическую обфускацию AWG
      if (
        [
          'jc',
          'jmin',
          'jmax',
          's1',
          's2',
          's3',
          's4',
          'h1',
          'h2',
          'h3',
          'h4',
          'i1',
          'i2',
          'i3',
          'i4',
          'i5',
          'headerprotectionkey',
          'version',
          'randomtrailers',
          'disablecookies',
          'rekeyaftertime'
        ].includes(lowerKey)
      ) {
        dropped.push({ key: k, val, reasonKey: 'proxies.diff_xray_dropped' });
      } else {
        kept.push({ key: k, val });
      }
      continue;
    }

    // Mihomo target
    const is31Key = [
      'version',
      'headerprotectionkey',
      'randomtrailers',
      'disablecookies',
      'rekeyaftertime',
      'i1',
      'i2',
      'i3',
      'i4',
      'i5'
    ].includes(lowerKey);

    if (is31Key && !supports31) {
      dropped.push({ key: k, val, reasonKey: 'proxies.diff_mihomo_31_unsupported' });
      continue;
    }

    // Проверка ограничений (warnings)
    if (lowerKey === 'jmax' && typeof val === 'number') {
      if (val + 80 > mtu) {
        warnings.push({ key: k, val, messageKey: 'proxies.diff_jmax_fragmentation' });
        continue;
      }
    }

    if (lowerKey === 's1' && typeof val === 'number' && rawAwg.headerProtectionKey) {
      if (val < 12) {
        warnings.push({ key: k, val, messageKey: 'proxies.diff_s1_too_small' });
        continue;
      }
    }

    kept.push({ key: k, val });
  }

  return { kept, warnings, dropped };
}
