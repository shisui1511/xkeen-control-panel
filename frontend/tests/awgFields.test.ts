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
  isMihomoAwg31Supported
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
});
