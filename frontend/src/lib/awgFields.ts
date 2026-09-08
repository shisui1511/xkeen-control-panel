/**
 * awgFields.ts — Единый реестр полей протокола AmneziaWG (Classic, 2.0, 3.1).
 * Централизованный источник истины для схем валидации, UI-форм, генератора и парсера YAML/INI.
 */

export type AwgTier = 'classic' | '1.5' | '2.0' | '3.1';
export type AwgFieldType = 'integer' | 'string' | 'boolean' | 'range' | 'hex';

export interface AwgFieldDef {
  key: string; // YAML ключ внутри блока amnezia-wg-option (например, 'header-protection-key')
  iniKey: string; // Имя в .conf wg-quick / INI (например, 'HeaderProtectionKey')
  propName: string; // Имя поля в объекте ProxyConfigState (например, 'awgHeaderProtectionKey')
  tier: AwgTier;
  type: AwgFieldType;
  default?: number | string | boolean;
  min?: number;
  max?: number;
  secret?: boolean;
  labelKey: string; // Ключ локализации
  hintKey?: string; // Ключ подсказки
}

export const AWG_FIELDS: readonly AwgFieldDef[] = [
  // ---------------------------------------------------------------------------
  // Tier 1: Classic (AWG 1.0 / 2.0 Base)
  // ---------------------------------------------------------------------------
  {
    key: 'jc',
    iniKey: 'Jc',
    propName: 'awgJc',
    tier: 'classic',
    type: 'integer',
    default: 4,
    min: 1,
    max: 128,
    labelKey: 'proxies.awg_jc'
  },
  {
    key: 'jmin',
    iniKey: 'Jmin',
    propName: 'awgJmin',
    tier: 'classic',
    type: 'integer',
    default: 40,
    min: 0,
    max: 1280,
    labelKey: 'proxies.awg_jmin'
  },
  {
    key: 'jmax',
    iniKey: 'Jmax',
    propName: 'awgJmax',
    tier: 'classic',
    type: 'integer',
    default: 70,
    min: 0,
    max: 1280,
    labelKey: 'proxies.awg_jmax'
  },
  {
    key: 's1',
    iniKey: 'S1',
    propName: 'awgS1',
    tier: 'classic',
    type: 'integer',
    default: 15,
    min: 0,
    max: 1280,
    labelKey: 'proxies.awg_s1'
  },
  {
    key: 's2',
    iniKey: 'S2',
    propName: 'awgS2',
    tier: 'classic',
    type: 'integer',
    default: 40,
    min: 0,
    max: 1280,
    labelKey: 'proxies.awg_s2'
  },
  {
    key: 'h1',
    iniKey: 'H1',
    propName: 'awgH1',
    tier: 'classic',
    type: 'range',
    default: 1000000001,
    min: 5,
    labelKey: 'proxies.awg_h1'
  },
  {
    key: 'h2',
    iniKey: 'H2',
    propName: 'awgH2',
    tier: 'classic',
    type: 'range',
    default: 1000000002,
    min: 5,
    labelKey: 'proxies.awg_h2'
  },
  {
    key: 'h3',
    iniKey: 'H3',
    propName: 'awgH3',
    tier: 'classic',
    type: 'range',
    default: 1000000003,
    min: 5,
    labelKey: 'proxies.awg_h3'
  },
  {
    key: 'h4',
    iniKey: 'H4',
    propName: 'awgH4',
    tier: 'classic',
    type: 'range',
    default: 1000000004,
    min: 5,
    labelKey: 'proxies.awg_h4'
  },

  // ---------------------------------------------------------------------------
  // Tier 1.5: AWG 1.5 Jitter & Packet Extensions
  // ---------------------------------------------------------------------------
  {
    key: 'j1',
    iniKey: 'J1',
    propName: 'awgJ1',
    tier: '1.5',
    type: 'integer',
    min: 0,
    max: 1280,
    labelKey: 'proxies.awg_j1'
  },
  {
    key: 'j2',
    iniKey: 'J2',
    propName: 'awgJ2',
    tier: '1.5',
    type: 'integer',
    min: 0,
    max: 1280,
    labelKey: 'proxies.awg_j2'
  },
  {
    key: 'j3',
    iniKey: 'J3',
    propName: 'awgJ3',
    tier: '1.5',
    type: 'integer',
    min: 0,
    max: 1280,
    labelKey: 'proxies.awg_j3'
  },
  {
    key: 'itime',
    iniKey: 'Itime',
    propName: 'awgItime',
    tier: '1.5',
    type: 'integer',
    min: 0,
    max: 65535,
    labelKey: 'proxies.awg_itime'
  },

  // ---------------------------------------------------------------------------
  // Tier 2: AWG 2.0 Extended
  // ---------------------------------------------------------------------------
  {
    key: 's3',
    iniKey: 'S3',
    propName: 'awgS3',
    tier: '2.0',
    type: 'integer',
    min: 0,
    max: 1280,
    labelKey: 'proxies.awg_s3'
  },
  {
    key: 's4',
    iniKey: 'S4',
    propName: 'awgS4',
    tier: '2.0',
    type: 'integer',
    min: 0,
    max: 1280,
    labelKey: 'proxies.awg_s4'
  },

  // ---------------------------------------------------------------------------
  // Tier 3: AWG 3.1 Extended Protocol
  // ---------------------------------------------------------------------------
  {
    key: 'version',
    iniKey: 'Version',
    propName: 'awgVersion',
    tier: '3.1',
    type: 'string',
    labelKey: 'proxies.awg_version'
  },
  {
    key: 'header-protection-key',
    iniKey: 'HeaderProtectionKey',
    propName: 'awgHeaderProtectionKey',
    tier: '3.1',
    type: 'string',
    secret: true,
    labelKey: 'proxies.awg_header_protection_key'
  },
  {
    key: 'i1',
    iniKey: 'I1',
    propName: 'awgI1',
    tier: '3.1',
    type: 'hex',
    labelKey: 'proxies.awg_i1'
  },
  {
    key: 'i2',
    iniKey: 'I2',
    propName: 'awgI2',
    tier: '3.1',
    type: 'hex',
    labelKey: 'proxies.awg_i2'
  },
  {
    key: 'i3',
    iniKey: 'I3',
    propName: 'awgI3',
    tier: '3.1',
    type: 'hex',
    labelKey: 'proxies.awg_i3'
  },
  {
    key: 'i4',
    iniKey: 'I4',
    propName: 'awgI4',
    tier: '3.1',
    type: 'hex',
    labelKey: 'proxies.awg_i4'
  },
  {
    key: 'i5',
    iniKey: 'I5',
    propName: 'awgI5',
    tier: '3.1',
    type: 'hex',
    labelKey: 'proxies.awg_i5'
  },
  {
    key: 'content-padding-addition',
    iniKey: 'ContentPaddingAddition',
    propName: 'awgContentPaddingAddition',
    tier: '3.1',
    type: 'integer',
    min: 0,
    max: 255,
    labelKey: 'proxies.awg_content_padding_addition'
  },
  {
    key: 'random-trailers',
    iniKey: 'RandomTrailers',
    propName: 'awgRandomTrailers',
    tier: '3.1',
    type: 'boolean',
    labelKey: 'proxies.awg_random_trailers'
  },
  {
    key: 'disable-cookies',
    iniKey: 'DisableCookies',
    propName: 'awgDisableCookies',
    tier: '3.1',
    type: 'boolean',
    labelKey: 'proxies.awg_disable_cookies'
  },
  {
    key: 'rekey-after-time',
    iniKey: 'RekeyAfterTime',
    propName: 'awgRekeyAfterTime',
    tier: '3.1',
    type: 'integer',
    min: 0,
    labelKey: 'proxies.awg_rekey_after_time'
  },
  {
    key: 'rekey-timeout',
    iniKey: 'RekeyTimeout',
    propName: 'awgRekeyTimeout',
    tier: '3.1',
    type: 'integer',
    min: 0,
    labelKey: 'proxies.awg_rekey_timeout'
  },
  {
    key: 'reject-after-time',
    iniKey: 'RejectAfterTime',
    propName: 'awgRejectAfterTime',
    tier: '3.1',
    type: 'integer',
    min: 0,
    labelKey: 'proxies.awg_reject_after_time'
  },
  {
    key: 'keepalive-timeout',
    iniKey: 'KeepaliveTimeout',
    propName: 'awgKeepaliveTimeout',
    tier: '3.1',
    type: 'integer',
    min: 0,
    labelKey: 'proxies.awg_keepalive_timeout'
  },
  {
    key: 'max-handshake-attempts',
    iniKey: 'MaxHandshakeAttempts',
    propName: 'awgMaxHandshakeAttempts',
    tier: '3.1',
    type: 'integer',
    min: 0,
    labelKey: 'proxies.awg_max_handshake_attempts'
  }
];

/**
 * Возвращает поля AWG для заданного тира.
 */
export function getAwgFieldsByTier(tier: AwgTier): AwgFieldDef[] {
  return AWG_FIELDS.filter((f) => f.tier === tier);
}

/**
 * Находит описание поля по YAML или INI ключу (регистронезависимо).
 */
export function findAwgField(key: string): AwgFieldDef | undefined {
  const norm = key.trim().toLowerCase();
  return AWG_FIELDS.find((f) => f.key.toLowerCase() === norm || f.iniKey.toLowerCase() === norm);
}

/**
 * Проверяет, является ли поле секретным.
 */
export function isAwgSecret(key: string): boolean {
  const field = findAwgField(key);
  return field?.secret === true;
}

/**
 * Нормализует значение поля (например, I1..I5 переводятся в UPPERCASE, удаляются пробелы).
 */
export function normalizeAwgValue(key: string, val: any): any {
  if (val === null || val === undefined) return val;
  const field = findAwgField(key);
  if (!field) return val;

  if (field.type === 'hex' && typeof val === 'string') {
    // I1..I5 — это шаблоны генерации junk-пакетов AmneziaWG с регистрозависимыми
    // CPS-тегами (<b 0x…>, <c>, <r N>, <t>). Приведение к UPPERCASE разрушает синтаксис,
    // а регистр hex-байт семантически безразличен — поэтому не трогаем регистр вообще.
    return val.trim();
  }
  if (field.type === 'integer') {
    if (typeof val === 'number') return Math.floor(val);
    if (typeof val === 'string' && val.trim() !== '') {
      const parsed = parseInt(val.trim(), 10);
      return isNaN(parsed) ? val : parsed;
    }
  }
  if (field.type === 'range') {
    if (typeof val === 'number') return Math.floor(val);
    if (typeof val === 'string') {
      const trimmed = val.trim();
      if (/^\d+$/.test(trimmed)) {
        const parsed = parseInt(trimmed, 10);
        return isNaN(parsed) ? trimmed : parsed;
      }
      if (/^\d+\s*-\s*\d+$/.test(trimmed)) {
        const parts = trimmed.split('-').map((s) => s.trim());
        const min = parseInt(parts[0], 10);
        const max = parseInt(parts[1], 10);
        if (!isNaN(min) && !isNaN(max)) {
          return min <= max ? `${min}-${max}` : `${max}-${min}`;
        }
      }
      return trimmed;
    }
  }
  if (field.type === 'boolean') {
    if (typeof val === 'boolean') return val;
    if (typeof val === 'string') {
      const lower = val.trim().toLowerCase();
      if (lower === 'true' || lower === 'yes' || lower === '1') return true;
      if (lower === 'false' || lower === 'no' || lower === '0') return false;
    }
    return undefined;
  }
  if (typeof val === 'string') {
    return val.trim();
  }
  return val;
}

/**
 * Проверяет, поддерживается ли AmneziaWG 3.1 в заданной версии ядра Mihomo.
 * Минимальная поддерживаемая версия для 3.0/3.1 — 1.19.30.
 */
export function isMihomoAwg31Supported(version?: string | null): boolean {
  if (!version) return false;
  // Очистка префикса 'v'
  const clean = version.trim().replace(/^v/i, '');
  const parts = clean.split('.').map((p) => parseInt(p, 10));
  if (parts.length < 2 || isNaN(parts[0]) || isNaN(parts[1])) return false;

  const major = parts[0];
  const minor = parts[1];
  const patch = parts.length > 2 && !isNaN(parts[2]) ? parts[2] : 0;

  if (major > 1) return true;
  if (major === 1) {
    if (minor > 19) return true;
    if (minor === 19) return patch >= 30;
  }
  return false;
}

export type WireGuardDialect = 'plain' | 'classic' | '1.5' | '2.0' | '3.1';

/**
 * Определяет диалект конфигурации WireGuard / AmneziaWG:
 * - 'plain': стандартный WireGuard
 * - 'classic': AmneziaWG 1.0 (jc, jmin, jmax, s1, s2, h1..h4)
 * - '1.5': AmneziaWG 1.5 (j1, j2, j3, itime)
 * - '2.0': AmneziaWG 2.0 (s3, s4)
 * - '3.1': AmneziaWG 3.1 (version, header-protection-key, i1..i5, etc.)
 */
export function detectWireGuardDialect(node: {
  dialect?: string;
  protocol?: string;
  awg?: any;
}): WireGuardDialect {
  if (node.dialect && ['plain', 'classic', '1.5', '2.0', '3.1'].includes(node.dialect)) {
    return node.dialect as WireGuardDialect;
  }
  const awg = node.awg;
  if (!awg) return 'plain';

  const rawVer =
    awg.version !== undefined && awg.version !== null ? String(awg.version).trim() : '';
  const isV3 = rawVer.startsWith('3') || rawVer.toLowerCase().startsWith('v3');

  // 3.1
  if (
    isV3 ||
    awg.header_protection_key ||
    awg.headerProtectionKey ||
    awg.i1 ||
    awg.i2 ||
    awg.i3 ||
    awg.i4 ||
    awg.i5 ||
    awg.content_padding_addition !== undefined ||
    awg.contentPaddingAddition !== undefined ||
    awg.random_trailers !== undefined ||
    awg.randomTrailers !== undefined ||
    awg.disable_cookies !== undefined ||
    awg.disableCookies !== undefined ||
    awg.rekey_after_time !== undefined ||
    awg.rekeyAfterTime !== undefined ||
    awg.rekey_timeout !== undefined ||
    awg.rekeyTimeout !== undefined ||
    awg.reject_after_time !== undefined ||
    awg.rejectAfterTime !== undefined ||
    awg.keepalive_timeout !== undefined ||
    awg.keepaliveTimeout !== undefined ||
    awg.max_handshake_attempts !== undefined ||
    awg.maxHandshakeAttempts !== undefined ||
    (awg.raw_options && Object.keys(awg.raw_options).length > 0)
  ) {
    return '3.1';
  }

  // 2.0
  const isV2 = rawVer.startsWith('2') || rawVer.toLowerCase().startsWith('v2');
  if (isV2 || awg.s3 !== undefined || awg.s4 !== undefined) {
    return '2.0';
  }

  // 1.5
  if (
    awg.j1 !== undefined ||
    awg.j2 !== undefined ||
    awg.j3 !== undefined ||
    awg.itime !== undefined
  ) {
    return '1.5';
  }

  // Classic
  if (
    awg.jc !== undefined ||
    awg.jmin !== undefined ||
    awg.jmax !== undefined ||
    awg.s1 !== undefined ||
    awg.s2 !== undefined ||
    awg.h1 !== undefined ||
    awg.h2 !== undefined ||
    awg.h3 !== undefined ||
    awg.h4 !== undefined
  ) {
    return 'classic';
  }

  return 'plain';
}
