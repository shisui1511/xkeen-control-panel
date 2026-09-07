/**
 * awgFields.test.ts — Unit-тесты для единого реестра полей AmneziaWG (awgFields.ts).
 */

import { describe, it, expect } from 'vitest';
import {
  AWG_FIELDS,
  getAwgFieldsByTier,
  findAwgField,
  isAwgSecret,
  normalizeAwgValue,
  isMihomoAwg31Supported,
  detectWireGuardDialect
} from '../src/lib/awgFields';

describe('AWG Fields Registry', () => {
  it('содержит корректное распределение по тирам', () => {
    const classic = getAwgFieldsByTier('classic');
    const tier2 = getAwgFieldsByTier('2.0');
    const tier31 = getAwgFieldsByTier('3.1');

    expect(classic.length).toBe(9); // jc, jmin, jmax, s1, s2, h1, h2, h3, h4
    expect(tier2.length).toBe(2); // s3, s4
    expect(tier31.length).toBe(11); // version, header-protection-key, i1..i5, content-padding-addition, random-trailers, disable-cookies, rekey-after-time
    expect(AWG_FIELDS.length).toBe(22);
  });

  it('корректно находит поля по YAML и INI ключам', () => {
    expect(findAwgField('jc')?.iniKey).toBe('Jc');
    expect(findAwgField('JMIN')?.key).toBe('jmin');
    expect(findAwgField('header-protection-key')?.iniKey).toBe('HeaderProtectionKey');
    expect(findAwgField('HeaderProtectionKey')?.key).toBe('header-protection-key');
    expect(findAwgField('s3')?.tier).toBe('2.0');
    expect(findAwgField('i1')?.tier).toBe('3.1');
    expect(findAwgField('unknown-field')).toBeUndefined();
  });

  it('верно идентифицирует секретные поля', () => {
    expect(isAwgSecret('header-protection-key')).toBe(true);
    expect(isAwgSecret('HeaderProtectionKey')).toBe(true);
    expect(isAwgSecret('jc')).toBe(false);
    expect(isAwgSecret('s1')).toBe(false);
    expect(isAwgSecret('i1')).toBe(false);
  });

  it('нормализует значения полей', () => {
    // Hex поля нормализуются в UPPERCASE
    expect(normalizeAwgValue('i1', '0a1b2c')).toBe('0A1B2C');
    expect(normalizeAwgValue('I2', '  3d4e5f  ')).toBe('3D4E5F');

    // Числовые поля
    expect(normalizeAwgValue('jc', '4')).toBe(4);
    expect(normalizeAwgValue('s1', 15.8)).toBe(15);

    // Булевы поля
    expect(normalizeAwgValue('random-trailers', 'true')).toBe(true);
    expect(normalizeAwgValue('random-trailers', 'false')).toBe(false);
    expect(normalizeAwgValue('disable-cookies', true)).toBe(true);
  });

  it('корректно определяет поддержку AWG 3.1 по версии ядра Mihomo', () => {
    // Версии ниже 1.19.30 не поддерживают AWG 3.1
    expect(isMihomoAwg31Supported('1.18.12')).toBe(false);
    expect(isMihomoAwg31Supported('v1.19.0')).toBe(false);
    expect(isMihomoAwg31Supported('1.19.29')).toBe(false);
    expect(isMihomoAwg31Supported(null)).toBe(false);
    expect(isMihomoAwg31Supported('')).toBe(false);

    // Версии начиная с 1.19.30 поддерживают AWG 3.1
    expect(isMihomoAwg31Supported('1.19.30')).toBe(true);
    expect(isMihomoAwg31Supported('v1.19.31')).toBe(true);
    expect(isMihomoAwg31Supported('1.20.0')).toBe(true);
    expect(isMihomoAwg31Supported('2.0.0')).toBe(true);
  });

  it('все поля AWG и коды предстартовой проверки переведены в ru.json и en.json', async () => {
    const ru = (await import('../src/locales/ru.json')).default as Record<string, string>;
    const en = (await import('../src/locales/en.json')).default as Record<string, string>;

    // Проверка labelKey всех полей реестра
    for (const field of AWG_FIELDS) {
      expect(ru[field.labelKey], `Отсутствует перевод ${field.labelKey} в ru.json`).toBeDefined();
      expect(en[field.labelKey], `Отсутствует перевод ${field.labelKey} в en.json`).toBeDefined();
    }

    // Проверка кодов предстартовой валидации
    const preflightCodes = [
      'preflight.awg_flat_fields',
      'preflight.awg_jmin_jmax',
      'preflight.awg_s1_s2',
      'preflight.awg_h_min',
      'preflight.awg_h_unique',
      'preflight.awg_s_header_protection',
      'preflight.awg_junk_mtu',
      'preflight.awg_version_incompatible',
      'preflight.awg_i_token',
      'preflight.awg_random_trailers'
    ];

    for (const code of preflightCodes) {
      expect(ru[code], `Отсутствует перевод ${code} в ru.json`).toBeDefined();
      expect(en[code], `Отсутствует перевод ${code} в en.json`).toBeDefined();
    }

    // Проверка переводов диалектов и совместимости
    const dialectKeys = [
      'subscr.dialect_plain',
      'subscr.dialect_classic',
      'subscr.dialect_20',
      'subscr.dialect_31',
      'subscr.awg_preserved_mihomo',
      'subscr.awg_incompatible_xray'
    ];
    for (const dk of dialectKeys) {
      expect(ru[dk], `Отсутствует перевод ${dk} в ru.json`).toBeDefined();
      expect(en[dk], `Отсутствует перевод ${dk} в en.json`).toBeDefined();
    }
  });

  it('корректно определяет диалекты WireGuard / AmneziaWG', () => {
    expect(detectWireGuardDialect({})).toBe('plain');
    expect(detectWireGuardDialect({ dialect: '2.0' })).toBe('2.0');

    // Classic
    expect(detectWireGuardDialect({ awg: { jc: 4, h1: 1000000001 } })).toBe('classic');

    // 2.0
    expect(detectWireGuardDialect({ awg: { jc: 4, s3: 20 } })).toBe('2.0');

    // 3.1
    expect(detectWireGuardDialect({ awg: { jc: 4, s3: 20, version: '3.1' } })).toBe('3.1');
    expect(detectWireGuardDialect({ awg: { header_protection_key: 'secret' } })).toBe('3.1');
    expect(detectWireGuardDialect({ awg: { i1: '0A1B2C' } })).toBe('3.1');
    expect(detectWireGuardDialect({ awg: { random_trailers: true } })).toBe('3.1');
  });
});
