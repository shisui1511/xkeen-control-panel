import { describe, it, expect } from 'vitest';
import { validatePasswordPolicy, policyErrorKey, utf8ByteLength } from './passwordPolicy';
import blacklist from './passwordBlacklist.json';

describe('validatePasswordPolicy', () => {
  it('короче 8 байт — password_too_short', () => {
    expect(validatePasswordPolicy('1234567')).toBe('password_too_short');
  });

  it('8-72 байта без нарушений — null', () => {
    expect(validatePasswordPolicy('12345678ab')).toBeNull();
  });

  it('73 ASCII-символа — password_too_long', () => {
    const password = 'a1'.repeat(36) + 'a'; // 73 байта
    expect(utf8ByteLength(password)).toBe(73);
    expect(validatePasswordPolicy(password)).toBe('password_too_long');
  });

  it('ровно 72 ASCII-символа без нарушений — null', () => {
    const password = 'a1'.repeat(36); // 72 байта
    expect(utf8ByteLength(password)).toBe(72);
    expect(validatePasswordPolicy(password)).toBeNull();
  });

  it('36 повторяющихся «Ж» (72 байта) — password_repeated_char', () => {
    const password = 'Ж'.repeat(36);
    expect(utf8ByteLength(password)).toBe(72);
    expect(validatePasswordPolicy(password)).toBe('password_repeated_char');
  });

  it('36 различных кириллических символов (72 байта) — null', () => {
    const password = 'АБВГДЕЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯабвг';
    expect(Array.from(password)).toHaveLength(36);
    expect(utf8ByteLength(password)).toBe(72);
    expect(validatePasswordPolicy(password)).toBeNull();
  });

  it('37 различных кириллических символов (74 байта) — password_too_long', () => {
    const password = 'АБВГДЕЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯабвгд';
    expect(Array.from(password)).toHaveLength(37);
    expect(utf8ByteLength(password)).toBe(74);
    expect(validatePasswordPolicy(password)).toBe('password_too_long');
  });

  it('"aaaaaaaa" — password_repeated_char', () => {
    expect(validatePasswordPolicy('aaaaaaaa')).toBe('password_repeated_char');
  });

  it('"ЯЯЯЯЯЯЯЯ" — password_repeated_char', () => {
    expect(validatePasswordPolicy('ЯЯЯЯЯЯЯЯ')).toBe('password_repeated_char');
  });

  it('"Password" — password_blacklisted (без учёта регистра)', () => {
    expect(validatePasswordPolicy('Password')).toBe('password_blacklisted');
  });

  it('"KEENETIC" — password_blacklisted (без учёта регистра)', () => {
    expect(validatePasswordPolicy('KEENETIC')).toBe('password_blacklisted');
  });

  it('совпадение с текущим паролем — password_same_as_current', () => {
    expect(validatePasswordPolicy('same-pass-1', { current: 'same-pass-1' })).toBe(
      'password_same_as_current'
    );
  });

  it('без opts.current совпадение с прошлым значением не проверяется', () => {
    expect(validatePasswordPolicy('same-pass-1')).toBeNull();
  });
});

describe('policyErrorKey', () => {
  it('password_blacklisted -> auth.password_policy_blacklisted', () => {
    expect(policyErrorKey('password_blacklisted')).toBe('auth.password_policy_blacklisted');
  });

  it('password_too_short -> auth.password_short', () => {
    expect(policyErrorKey('password_too_short')).toBe('auth.password_short');
  });

  it('password_too_long -> auth.password_too_long', () => {
    expect(policyErrorKey('password_too_long')).toBe('auth.password_too_long');
  });

  it('password_repeated_char -> auth.password_policy_repeated_char', () => {
    expect(policyErrorKey('password_repeated_char')).toBe('auth.password_policy_repeated_char');
  });

  it('password_same_as_current -> auth.password_policy_same_as_current', () => {
    expect(policyErrorKey('password_same_as_current')).toBe('auth.password_policy_same_as_current');
  });

  it('setup_code_invalid -> auth.setup_code_invalid', () => {
    expect(policyErrorKey('setup_code_invalid')).toBe('auth.setup_code_invalid');
  });

  it('неизвестный код -> null', () => {
    expect(policyErrorKey('unknown')).toBeNull();
  });

  it('без аргумента -> null', () => {
    expect(policyErrorKey()).toBeNull();
  });
});

describe('passwordBlacklist.json', () => {
  it('47 элементов, все в нижнем регистре, без дублей', () => {
    expect(blacklist).toHaveLength(47);
    expect(new Set(blacklist).size).toBe(47);
    expect(blacklist.every((word) => word === word.toLowerCase())).toBe(true);
  });
});
