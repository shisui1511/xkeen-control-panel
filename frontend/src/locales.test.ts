/**
 * locales.test.ts — целостность словарей ru/en.
 *
 * JSON.parse молча оставляет последнее значение дублирующегося ключа, поэтому
 * дубль незаметно подменяет строку (например, теряются плейсхолдеры {count}).
 */

import { describe, it, expect } from 'vitest';
import ruRaw from './locales/ru.json?raw';
import enRaw from './locales/en.json?raw';

const RAW: Record<string, string> = { ru: ruRaw, en: enRaw };
const LOCALES = Object.keys(RAW);

function rawKeys(lang: string): string[] {
  return [...RAW[lang].matchAll(/^\s*"([^"]+)"\s*:/gm)].map((m) => m[1]);
}

function placeholders(value: string): string[] {
  return [...value.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();
}

describe('locales', () => {
  for (const lang of LOCALES) {
    it(`${lang}.json has no duplicate keys`, () => {
      const seen = new Set<string>();
      const dups = rawKeys(lang).filter((k) => (seen.has(k) ? true : (seen.add(k), false)));
      expect(dups).toEqual([]);
    });
  }

  it('ru and en use the same placeholders', () => {
    const ru = JSON.parse(ruRaw);
    const en = JSON.parse(enRaw);
    const mismatched = Object.keys(ru).filter(
      (k) => k in en && placeholders(ru[k]).join() !== placeholders(en[k]).join()
    );
    expect(mismatched).toEqual([]);
  });
});
