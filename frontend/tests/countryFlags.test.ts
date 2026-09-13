import { describe, it, expect } from 'vitest';
import {
  countryCodeToFlag,
  extractFlagEmoji,
  hasFlagEmoji,
  getCountryCode,
  getCountryFlag,
  getMissingCountryFlag,
  cleanNodeDisplayName,
  flagMap
} from '../src/lib/countryFlags';

describe('countryFlags utilities', () => {
  it('converts 2-letter ISO country codes to flag emojis', () => {
    expect(countryCodeToFlag('US')).toBe('🇺🇸');
    expect(countryCodeToFlag('de')).toBe('🇩🇪');
    expect(countryCodeToFlag('RU')).toBe('🇷🇺');
    expect(countryCodeToFlag('NL')).toBe('🇳🇱');
    expect(countryCodeToFlag('INVALID')).toBe('');
    expect(countryCodeToFlag('')).toBe('');
  });

  it('extracts existing flag emoji from node name', () => {
    const res = extractFlagEmoji('🇺🇸 США • Чикаго');
    expect(res).not.toBeNull();
    expect(res?.flag).toBe('🇺🇸');
    expect(res?.countryCode).toBe('US');

    const resFr = extractFlagEmoji('🇫🇷 Франция');
    expect(resFr?.flag).toBe('🇫🇷');
    expect(resFr?.countryCode).toBe('FR');

    expect(extractFlagEmoji('Direct Server')).toBeNull();
  });

  it('detects country codes from bracketed prefixes and tokens', () => {
    expect(getCountryCode('[US] Node 1')).toBe('US');
    expect(getCountryCode('(DE) Frankfurt')).toBe('DE');
    expect(getCountryCode('NL - Amsterdam 01')).toBe('NL');
    expect(getCountryCode('FR_Paris_Fast')).toBe('FR');
    expect(getCountryCode('UK - London')).toBe('GB');
    expect(getCountryCode('USA - Dallas')).toBe('US');
  });

  it('detects country codes from country and city names in EN and RU', () => {
    expect(getCountryCode('Frankfurt Fast')).toBe('DE');
    expect(getCountryCode('Германия 01')).toBe('DE');
    expect(getCountryCode('Нидерланды - 10Gbps')).toBe('NL');
    expect(getCountryCode('Amsterdam CDN')).toBe('NL');
    expect(getCountryCode('Tokyo Ultra')).toBe('JP');
    expect(getCountryCode('Япония - Токио')).toBe('JP');
    expect(getCountryCode('Warsaw Relay')).toBe('PL');
    expect(getCountryCode('Польша Варшава')).toBe('PL');
    expect(getCountryCode('Стокгольм')).toBe('SE');
    expect(getCountryCode('Stockholm 01')).toBe('SE');
    expect(getCountryCode('Helsinki Cloud')).toBe('FI');
    expect(getCountryCode('Almaty Direct')).toBe('KZ');
    expect(getCountryCode('Dubai VIP')).toBe('AE');
    expect(getCountryCode('Singapore Best')).toBe('SG');
    expect(getCountryCode('Hong Kong #4')).toBe('HK');
    expect(getCountryCode('Кипр Лимассол')).toBe('CY');
  });

  it('does not produce false positives on unrelated words', () => {
    expect(getCountryCode('Direct')).toBe('');
    expect(getCountryCode('Reject')).toBe('');
    expect(getCountryCode('GLOBAL')).toBe('');
    expect(getCountryCode('TrustServer')).toBe('');
    expect(getCountryCode('Forum-Test')).toBe('');
    expect(getCountryCode('RustVless')).toBe('');
  });

  it('returns valid flag emojis via getCountryFlag', () => {
    expect(getCountryFlag('🇺🇸 США • Чикаго')).toBe('🇺🇸');
    expect(getCountryFlag('DE - Frankfurt 01')).toBe('🇩🇪');
    expect(getCountryFlag('Россия - Москва')).toBe('🇷🇺');
    expect(getCountryFlag('Direct')).toBe('');
  });

  it('cleans node display name by stripping duplicate flag emojis', () => {
    expect(cleanNodeDisplayName('🇺🇸 США • Чикаго')).toBe('США • Чикаго');
    expect(cleanNodeDisplayName('🇫🇷 Франция')).toBe('Франция');
    expect(cleanNodeDisplayName('🇳🇱 Amsterdam 01 🇳🇱')).toBe('Amsterdam 01');
    expect(cleanNodeDisplayName('DE - Frankfurt 01')).toBe('DE - Frankfurt 01');
    expect(cleanNodeDisplayName('DIRECT')).toBe('DIRECT');
    // If string only had flag emoji, preserve something readable
    expect(cleanNodeDisplayName('🇺🇸')).toBe('🇺🇸');
  });

  it('handles hasFlagEmoji and getMissingCountryFlag without duplicate flags', () => {
    expect(hasFlagEmoji('🇺🇸 США • Чикаго')).toBe(true);
    expect(hasFlagEmoji('DE - Frankfurt 01')).toBe(false);
    expect(hasFlagEmoji('DIRECT')).toBe(false);

    // If node already has a flag, getMissingCountryFlag returns ''
    expect(getMissingCountryFlag('🇺🇸 США • Чикаго')).toBe('');
    expect(getMissingCountryFlag('🇫🇷 Франция')).toBe('');

    // If node lacks a flag, getMissingCountryFlag infers it
    expect(getMissingCountryFlag('DE - Frankfurt 01')).toBe('🇩🇪');
    expect(getMissingCountryFlag('Нидерланды - 10Gbps')).toBe('🇳🇱');
    expect(getMissingCountryFlag('DIRECT')).toBe('');
  });

  it('preserves flagMap backward compatibility', () => {
    expect(flagMap['RU']).toBe('🇷🇺');
    expect(flagMap['Germany']).toBe('🇩🇪');
    expect(flagMap['США']).toBe('🇺🇸');
    expect(flagMap['NonExistentCountry123']).toBe('');
  });
});
